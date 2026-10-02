package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/server"
	"github.com/tristanisham/violet/server/ai"
)

// fakeGateway stands in for Cloudflare so tests never make network requests.
type fakeGateway struct {
	mu     sync.Mutex
	models []protocol.CatalogModel
}

func (f *fakeGateway) LoadModels(context.Context) ([]protocol.CatalogModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.models, nil
}

func (f *fakeGateway) RunChat(context.Context, string, ai.ChatRequest) (json.RawMessage, error) {
	return json.RawMessage(`{"done":true}`), nil
}

// startServer runs a real engine behind an httptest server. Set any
// VIOLET_SERVER_TOKEN env var before calling, since Handler reads it once.
func startServer(t *testing.T) (*server.Engine, *httptest.Server, *fakeGateway) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	t.Setenv("AppData", root)
	engine := server.NewEngine()
	fake := &fakeGateway{}
	engine.LoadModels, engine.RunChat = fake.LoadModels, fake.RunChat
	state := &meta.State{Config: &meta.Config{ProjectDir: t.TempDir()}, GraphicSettings: *meta.NewGraphicSettings()}
	if err := engine.Start(state); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(engine.Handler(state))
	// Cleanups run LIFO: stop the engine first so streams end, then close the server.
	t.Cleanup(ts.Close)
	t.Cleanup(engine.Stop)
	return engine, ts, fake
}

func newClient(t *testing.T, base, token string) protocol.Client {
	t.Helper()
	c, err := NewHTTP(base, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func subscribe(t *testing.T, c protocol.Client) <-chan protocol.Event {
	t.Helper()
	events, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func request(t *testing.T, subject protocol.Subject, recipient string, payload any) protocol.Request {
	t.Helper()
	req, err := protocol.NewRequest(subject, recipient, payload)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func collect(t *testing.T, events <-chan protocol.Event, count int) []protocol.Event {
	t.Helper()
	got := make([]protocol.Event, 0, count)
	deadline := time.After(5 * time.Second)
	for len(got) < count {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("stream closed early after %+v", got)
			}
			got = append(got, event)
		case <-deadline:
			t.Fatalf("timed out: got %d of %d events: %+v", len(got), count, got)
		}
	}
	return got
}

func expectClosed(t *testing.T, events <-chan protocol.Event) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("event channel was not closed")
		}
	}
}

func chatInput(content string) protocol.ChatInput {
	return protocol.ChatInput{Messages: []protocol.ChatMessage{{Role: "user", Content: content}}}
}

func TestHTTPClientEndToEnd(t *testing.T) {
	_, ts, fake := startServer(t)
	fake.models = []protocol.CatalogModel{{Name: "alpha"}}
	alice := newClient(t, ts.URL+"/", "")
	bob := newClient(t, ts.URL, "")
	aliceEvents, bobEvents := subscribe(t, alice), subscribe(t, bob)

	chat := request(t, protocol.SubjectChat, "agent-1", chatInput("hello"))
	if err := alice.Submit(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	for name, events := range map[string]<-chan protocol.Event{"alice": aliceEvents, "bob": bobEvents} {
		got := collect(t, events, 2)
		if got[0].Kind != "chat.saved" || got[1].Kind != "chat.completed" {
			t.Fatalf("%s chat events = %+v", name, got)
		}
		for _, event := range got {
			if event.ClientID != "" || event.RequestID != chat.ID {
				t.Fatalf("%s: chat event not shared/correlated: %+v", name, event)
			}
		}
		if string(got[1].Data) != `{"done":true}` {
			t.Fatalf("%s: completion data = %s", name, got[1].Data)
		}
	}

	models := request(t, protocol.SubjectModels, "cloudflare", struct{}{})
	if err := bob.Submit(context.Background(), models); err != nil {
		t.Fatal(err)
	}
	reply := collect(t, bobEvents, 1)[0]
	if reply.RequestID != models.ID || reply.Kind != "result" || reply.ClientID == "" {
		t.Fatalf("models reply = %+v", reply)
	}
	var list []protocol.CatalogModel
	if err := json.Unmarshal(reply.Data, &list); err != nil || len(list) != 1 || list[0].Name != "alpha" {
		t.Fatalf("models payload = %s, %v", reply.Data, err)
	}
	// Private replies never reach other subscribers. A following private reply to
	// alice proves her stream is ordered past the point bob's reply was emitted.
	probe := request(t, protocol.SubjectGraphicsGet, "", struct{}{})
	if err := alice.Submit(context.Background(), probe); err != nil {
		t.Fatal(err)
	}
	if event := collect(t, aliceEvents, 1)[0]; event.RequestID != probe.ID {
		t.Fatalf("alice received another client's reply: %+v", event)
	}
}

func TestHTTPSubmitBeforeSubscribe(t *testing.T) {
	_, ts, _ := startServer(t)
	c := newClient(t, ts.URL, "")
	err := c.Submit(context.Background(), request(t, protocol.SubjectModels, "", struct{}{}))
	if !errors.Is(err, protocol.ErrDisconnected) {
		t.Fatalf("submit before subscribe = %v", err)
	}
	subscribe(t, c)
	if _, err := c.Subscribe(context.Background()); err == nil {
		t.Fatal("second subscription allowed")
	}
}

func TestHTTPTokenAuth(t *testing.T) {
	const token = "test-secret-token"
	t.Setenv("VIOLET_SERVER_TOKEN", token)
	_, ts, _ := startServer(t)
	for _, bad := range []string{"", "wrong-token"} {
		c := newClient(t, ts.URL, bad)
		_, err := c.Subscribe(context.Background())
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("token %q: subscribe = %v", bad, err)
		}
		if strings.Contains(err.Error(), bad) && bad != "" {
			t.Fatalf("error leaks token: %v", err)
		}
		err = c.Submit(context.Background(), request(t, protocol.SubjectModels, "", struct{}{}))
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("token %q: submit = %v", bad, err)
		}
	}
	c := newClient(t, ts.URL, token)
	events := subscribe(t, c)
	req := request(t, protocol.SubjectGraphicsGet, "", struct{}{})
	if err := c.Submit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if event := collect(t, events, 1)[0]; event.RequestID != req.ID {
		t.Fatalf("authorized reply = %+v", event)
	}
}

func TestNewHTTPValidatesURL(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080", "http://127.0.0.1:8080/", "http://127.9.9.9", "http://[::1]:80",
		"https://violet.example.com", "https://violet.example.com/base/",
	} {
		if _, err := NewHTTP(raw, ""); err != nil {
			t.Errorf("%s rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://violet.example.com", "http://10.0.0.1:8080", "https://user:pass@violet.example.com",
		"https://user@violet.example.com", "https://violet.example.com/?q=1", "https://violet.example.com/#frag",
		"https://violet.example.com/#", "ftp://localhost", "localhost:8080", "", "https://", "http:///path",
	} {
		if _, err := NewHTTP(raw, "secret"); err == nil {
			t.Errorf("%q accepted", raw)
		} else if strings.Contains(err.Error(), "pass") || strings.Contains(err.Error(), "secret") {
			t.Errorf("%q: error leaks credentials: %v", raw, err)
		}
	}
	c, err := NewHTTP("https://violet.example.com/base///", "")
	if err != nil {
		t.Fatal(err)
	}
	if base := c.(*httpClient).base; base != "https://violet.example.com/base" {
		t.Fatalf("base = %q", base)
	}
}

func TestHTTPCloseEndsStream(t *testing.T) {
	_, ts, _ := startServer(t)
	c := newClient(t, ts.URL, "")
	events := subscribe(t, c)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	expectClosed(t, events)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Submit(context.Background(), request(t, protocol.SubjectModels, "", struct{}{})); !errors.Is(err, protocol.ErrDisconnected) {
		t.Fatalf("submit after close = %v", err)
	}
	if _, err := c.Subscribe(context.Background()); !errors.Is(err, protocol.ErrDisconnected) {
		t.Fatalf("subscribe after close = %v", err)
	}
}

func TestHTTPContextCancelEndsStream(t *testing.T) {
	_, ts, _ := startServer(t)
	c := newClient(t, ts.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	events, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	expectClosed(t, events)
}

func TestHTTPServerStopEndsStream(t *testing.T) {
	engine, ts, _ := startServer(t)
	c := newClient(t, ts.URL, "")
	events := subscribe(t, c)
	engine.Stop()
	expectClosed(t, events)
	if _, err := c.Subscribe(context.Background()); !errors.Is(err, protocol.ErrStopped) {
		t.Fatalf("subscribe to stopped engine = %v", err)
	}
	if err := c.Submit(context.Background(), request(t, protocol.SubjectModels, "", struct{}{})); !errors.Is(err, protocol.ErrStopped) {
		t.Fatalf("submit to stopped engine = %v", err)
	}
}

func TestReadEventsParsesSSE(t *testing.T) {
	stream := ": keepalive\n\n" +
		"event: ignored\ndata: {\"id\":\"1\",\n" +
		"data: \"kind\":\"a\"}\n\n" +
		"data:{\"id\":\"2\",\"kind\":\"b\"}\r\n\r\n" +
		"\n\n" +
		"data: not json\n\n" +
		"data: {\"id\":\"3\"}\n\n"
	events := make(chan protocol.Event, 8)
	readEvents(context.Background(), strings.NewReader(stream), events, func() {})
	close(events)
	var got []protocol.Event
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 2 || got[0].ID != "1" || got[0].Kind != "a" || got[1].ID != "2" || got[1].Kind != "b" {
		t.Fatalf("parsed events = %+v", got)
	}
}
