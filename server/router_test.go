package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/server/ai"
)

// fakeGateway stands in for Cloudflare so tests never make network requests.
type fakeGateway struct {
	mu       sync.Mutex
	models   []protocol.CatalogModel
	modelErr error
	order    []string
	release  chan struct{} // when non-nil, RunChat blocks until it is closed
}

func (f *fakeGateway) LoadModels(ctx context.Context) ([]protocol.CatalogModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.models, f.modelErr
}

func (f *fakeGateway) RunChat(ctx context.Context, model string, chat ai.ChatRequest) (json.RawMessage, error) {
	f.mu.Lock()
	f.order = append(f.order, chat.Messages[0].Content)
	release := f.release
	f.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return json.RawMessage(`{"done":true}`), nil
}

func isolateSettings(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	t.Setenv("AppData", root)
}

func newTestState(t *testing.T) *meta.State {
	t.Helper()
	return &meta.State{Config: &meta.Config{ProjectDir: t.TempDir()}, GraphicSettings: *meta.NewGraphicSettings()}
}

func newStartedEngine(t *testing.T) (*Engine, *fakeGateway) {
	t.Helper()
	isolateSettings(t)
	engine := NewEngine()
	fake := &fakeGateway{}
	engine.LoadModels, engine.RunChat = fake.LoadModels, fake.RunChat
	if err := engine.Start(newTestState(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Stop)
	return engine, fake
}

func attach(t *testing.T, engine *Engine) (protocol.Client, <-chan protocol.Event) {
	t.Helper()
	client := NewLocalClient(engine)
	events, err := client.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client, events
}

func submit(t *testing.T, client protocol.Client, subject protocol.Subject, recipient, conversation string, payload any) protocol.Request {
	t.Helper()
	req, err := protocol.NewRequest(subject, recipient, payload)
	if err != nil {
		t.Fatal(err)
	}
	req.ConversationID = conversation
	if err := client.Submit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	return req
}

func collectEvents(t *testing.T, events <-chan protocol.Event, count int) []protocol.Event {
	t.Helper()
	got := make([]protocol.Event, 0, count)
	deadline := time.After(5 * time.Second)
	for len(got) < count {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("subscription closed early after %+v", got)
			}
			got = append(got, event)
		case <-deadline:
			t.Fatalf("timed out waiting for events: got %d of %d: %+v", len(got), count, got)
		}
	}
	return got
}

func expectNoEvent(t *testing.T, events <-chan protocol.Event) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected event %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func chatInput(content string) protocol.ChatInput {
	return protocol.ChatInput{Messages: []protocol.ChatMessage{{Role: "user", Content: content}}}
}

func TestSubmitRequiresSubscription(t *testing.T) {
	engine, _ := newStartedEngine(t)
	req, err := protocol.NewRequest(protocol.SubjectModels, "", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Submit(context.Background(), "missing", req); !errors.Is(err, protocol.ErrDisconnected) {
		t.Fatalf("unconnected submit = %v", err)
	}
	client := NewLocalClient(engine)
	defer client.Close()
	if err := client.Submit(context.Background(), req); !errors.Is(err, protocol.ErrDisconnected) {
		t.Fatalf("submit before subscribe = %v", err)
	}
	first, err := client.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Resubscribing replaces the old subscription, which is closed.
	if _, err := client.Subscribe(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-first:
		if ok {
			t.Fatal("replaced subscription delivered an event")
		}
	case <-time.After(time.Second):
		t.Fatal("replaced subscription was not closed")
	}
	if err := client.Submit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	bad := req
	bad.Subject = "nope"
	if err := client.Submit(context.Background(), bad); err == nil {
		t.Fatal("invalid subject accepted")
	}
	client.Close()
	if err := client.Submit(context.Background(), req); !errors.Is(err, protocol.ErrDisconnected) {
		t.Fatalf("submit after close = %v", err)
	}
}

func TestStoppedEngineRejectsClients(t *testing.T) {
	engine := NewEngine()
	if _, err := NewLocalClient(engine).Subscribe(context.Background()); !errors.Is(err, protocol.ErrStopped) {
		t.Fatalf("subscribe to stopped engine = %v", err)
	}
	engine, _ = newStartedEngine(t)
	_, events := attach(t, engine)
	engine.Stop()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected event on shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not close subscriptions")
	}
}

func TestModelsReplyIsPrivateAndCorrelated(t *testing.T) {
	engine, fake := newStartedEngine(t)
	fake.models = []protocol.CatalogModel{{Name: "alpha"}, {Name: "zeta"}}
	requester, events := attach(t, engine)
	_, otherEvents := attach(t, engine)
	req := submit(t, requester, protocol.SubjectModels, "cloudflare", "", struct{}{})
	event := collectEvents(t, events, 1)[0]
	if event.RequestID != req.ID || event.Kind != "result" || event.Error != "" {
		t.Fatalf("models event = %+v", event)
	}
	var models []protocol.CatalogModel
	if err := json.Unmarshal(event.Data, &models); err != nil || len(models) != 2 {
		t.Fatalf("models payload = %s, %v", event.Data, err)
	}
	expectNoEvent(t, otherEvents)

	fake.mu.Lock()
	fake.modelErr = errors.New("catalog down")
	fake.mu.Unlock()
	req = submit(t, requester, protocol.SubjectModels, "cloudflare", "", struct{}{})
	event = collectEvents(t, events, 1)[0]
	if event.RequestID != req.ID || event.Kind != "error" || event.Error != "catalog down" {
		t.Fatalf("models error event = %+v", event)
	}
}

func TestChatEventsAreSharedAndLanesAreOrdered(t *testing.T) {
	engine, fake := newStartedEngine(t)
	alice, aliceEvents := attach(t, engine)
	_, bobEvents := attach(t, engine)
	for _, content := range []string{"one", "two", "three"} {
		submit(t, alice, protocol.SubjectChat, "agent-1", "conv-1", chatInput(content))
	}
	for _, events := range []<-chan protocol.Event{aliceEvents, bobEvents} {
		saved, completed := 0, 0
		for _, event := range collectEvents(t, events, 6) {
			if event.ClientID != "" {
				t.Fatalf("chat event was not shared: %+v", event)
			}
			switch event.Kind {
			case "chat.saved":
				saved++
			case "chat.completed":
				completed++
			default:
				t.Fatalf("unexpected chat event %+v", event)
			}
		}
		if saved != 3 || completed != 3 {
			t.Fatalf("events = %d saved, %d completed", saved, completed)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.order) != 3 || fake.order[0] != "one" || fake.order[1] != "two" || fake.order[2] != "three" {
		t.Fatalf("lane order = %v", fake.order)
	}
}

func TestIndependentAgentsRunConcurrently(t *testing.T) {
	engine, fake := newStartedEngine(t)
	fake.release = make(chan struct{})
	client, events := attach(t, engine)
	submit(t, client, protocol.SubjectChat, "agent-1", "", chatInput("slow"))
	submit(t, client, protocol.SubjectChat, "agent-2", "", chatInput("fast"))
	// Both agents reach the provider while the first is still blocked.
	if got := collectEvents(t, events, 2); got[0].Kind != "chat.saved" || got[1].Kind != "chat.saved" {
		t.Fatalf("expected both chats saved first: %+v", got)
	}
	deadline := time.Now().Add(time.Second)
	for {
		fake.mu.Lock()
		running := len(fake.order)
		fake.mu.Unlock()
		if running == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agents did not run concurrently")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A settings request is not stuck behind blocked chats.
	submit(t, client, protocol.SubjectGraphicsGet, "", "", struct{}{})
	if event := collectEvents(t, events, 1)[0]; event.Subject != protocol.SubjectGraphicsGet {
		t.Fatalf("settings blocked by chat: %+v", event)
	}
	close(fake.release)
	collectEvents(t, events, 2)
}

func TestChatValidationFailsPrivately(t *testing.T) {
	engine, _ := newStartedEngine(t)
	client, events := attach(t, engine)
	_, otherEvents := attach(t, engine)
	req := submit(t, client, protocol.SubjectChat, "", "", chatInput("no agent"))
	if event := collectEvents(t, events, 1)[0]; event.Kind != "error" || event.RequestID != req.ID {
		t.Fatalf("missing recipient event = %+v", event)
	}
	submit(t, client, protocol.SubjectChat, "agent", "", "not an object")
	if event := collectEvents(t, events, 1)[0]; event.Kind != "error" {
		t.Fatalf("malformed chat event = %+v", event)
	}
	expectNoEvent(t, otherEvents)
}

func TestSettingsRequestsPersistThroughServer(t *testing.T) {
	engine, _ := newStartedEngine(t)
	client, events := attach(t, engine)
	decode := func(event protocol.Event) meta.GraphicSettings {
		t.Helper()
		if event.Error != "" {
			t.Fatalf("settings error: %s", event.Error)
		}
		var settings meta.GraphicSettings
		if err := json.Unmarshal(event.Data, &settings); err != nil {
			t.Fatal(err)
		}
		return settings
	}
	submit(t, client, protocol.SubjectThemeSelect, "", "", map[string]string{"name": "Forest"})
	if got := decode(collectEvents(t, events, 1)[0]); got.Palette != "Forest" {
		t.Fatalf("theme select = %q", got.Palette)
	}
	palette := map[string]string{"primary": "#112233", "secondary": "#445566", "accent": "#778899", "background": "#000000"}
	submit(t, client, protocol.SubjectPaletteCreate, "", "", map[string]any{"name": "Night", "palette": palette})
	if got := decode(collectEvents(t, events, 1)[0]); got.Palette != "Night" || len(got.Palettes) != 7 {
		t.Fatalf("palette create = %+v", got)
	}
	saved, err := meta.LoadGraphicSettings()
	if err != nil || saved.Palette != "Night" {
		t.Fatalf("settings not persisted: %+v, %v", saved, err)
	}
	for _, payload := range []any{map[string]string{"name": "Missing"}, map[string]string{"name": " Bad "}} {
		submit(t, client, protocol.SubjectThemeSelect, "", "", payload)
		if event := collectEvents(t, events, 1)[0]; event.Kind != "error" {
			t.Fatalf("invalid theme accepted: %+v", event)
		}
	}
	submit(t, client, protocol.SubjectPaletteCreate, "", "", map[string]any{"name": "Night", "palette": palette})
	if event := collectEvents(t, events, 1)[0]; event.Kind != "error" {
		t.Fatalf("duplicate palette accepted: %+v", event)
	}
}

func TestSlowSubscriberIsDisconnected(t *testing.T) {
	engine, _ := newStartedEngine(t)
	_, events := attach(t, engine)
	client, healthy := attach(t, engine)
	// Never read the slow subscriber; chat events are shared so they fill its buffer.
	for i := range subscriberBuffer + 1 {
		submit(t, client, protocol.SubjectChat, "agent", string(rune('a'+i%26)), chatInput("hi"))
		collectEvents(t, healthy, 2) // saved, completed
	}
	drained := 0
	for range events {
		drained++
	}
	if drained > subscriberBuffer {
		t.Fatalf("slow subscriber received %d events beyond its buffer", drained)
	}
}

func TestConcurrentClientsAndLifecycle(t *testing.T) {
	engine, _ := newStartedEngine(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			client := NewLocalClient(engine)
			defer client.Close()
			events, err := client.Subscribe(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			req, _ := protocol.NewRequest(protocol.SubjectGraphicsGet, "", struct{}{})
			if err := client.Submit(context.Background(), req); err != nil {
				t.Error(err)
				return
			}
			select {
			case event := <-events:
				if event.RequestID != req.ID {
					t.Errorf("reply went to the wrong client: %+v", event)
				}
			case <-time.After(5 * time.Second):
				t.Error("timed out")
			}
		})
	}
	wg.Wait()
}

func TestWorkOutlivesCanceledSubmitContext(t *testing.T) {
	engine, _ := newStartedEngine(t)
	client, events := attach(t, engine)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := protocol.NewRequest(protocol.SubjectGraphicsGet, "", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Submit(ctx, req); err != nil {
		t.Fatal(err)
	}
	cancel() // a caller's deferred cancel must not abort accepted work
	if event := collectEvents(t, events, 1)[0]; event.Kind != "result" || event.Error != "" {
		t.Fatalf("work was canceled with the submit context: %+v", event)
	}
}

func TestDuplicateChatIDDoesNotOverwrite(t *testing.T) {
	engine, _ := newStartedEngine(t)
	client, events := attach(t, engine)
	req := submit(t, client, protocol.SubjectChat, "agent", "", chatInput("original"))
	collectEvents(t, events, 2)
	replay, err := protocol.NewRequest(protocol.SubjectChat, "agent", chatInput("overwrite"))
	if err != nil {
		t.Fatal(err)
	}
	replay.ID = req.ID
	if err := client.Submit(context.Background(), replay); err != nil {
		t.Fatal(err)
	}
	if event := collectEvents(t, events, 1)[0]; event.Kind != "error" || event.RequestID != req.ID {
		t.Fatalf("duplicate chat ID was accepted: %+v", event)
	}
}

func TestInvalidLoadedGraphicsRefuseToStart(t *testing.T) {
	isolateSettings(t)
	state := newTestState(t)
	state.GraphicSettings.Palette = "Missing"
	if err := NewEngine().Start(state); err == nil {
		t.Fatal("engine started with invalid settings that it would later overwrite")
	}
}
