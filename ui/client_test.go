package ui

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/ui/components"
	"github.com/tristanisham/violet/ui/pages"
)

// fakeClient records submissions; events are delivered by tests directly as
// eventMsg so no goroutine races the test for the channel.
type fakeClient struct {
	mu                    sync.Mutex
	events                chan protocol.Event
	requests              []protocol.Request
	subscribed            bool
	submitBeforeSubscribe bool
	submitErr             error
	subscribeErr          error
}

func newFakeClient(t *testing.T) *fakeClient {
	f := &fakeClient{events: make(chan protocol.Event)}
	t.Cleanup(func() { close(f.events) })
	return f
}

func (f *fakeClient) Submit(_ context.Context, req protocol.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := req.Validate(); err != nil {
		return err
	}
	if !f.subscribed {
		f.submitBeforeSubscribe = true
	}
	f.requests = append(f.requests, req)
	return f.submitErr
}

func (f *fakeClient) Subscribe(context.Context) (<-chan protocol.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subscribeErr != nil {
		return nil, f.subscribeErr
	}
	f.subscribed = true
	return f.events, nil
}

func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) sent() []protocol.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.Request(nil), f.requests...)
}

func (f *fakeClient) last(t *testing.T) protocol.Request {
	t.Helper()
	sent := f.sent()
	if len(sent) == 0 {
		t.Fatal("no request was submitted")
	}
	return sent[len(sent)-1]
}

// run executes cmd and feeds every produced message back into the model.
// Commands that block (the event listener) are abandoned after a short wait.
func run(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-result:
	case <-time.After(50 * time.Millisecond):
		return model
	}
	switch msg := msg.(type) {
	case nil:
		return model
	case tea.BatchMsg:
		for _, cmd := range msg {
			model = run(t, model, cmd)
		}
		return model
	case tea.QuitMsg:
		t.Fatal("model quit unexpectedly")
	}
	model, cmd = model.Update(msg)
	return run(t, model, cmd)
}

func update(t *testing.T, model tea.Model, msg tea.Msg) tea.Model {
	t.Helper()
	model, cmd := model.Update(msg)
	return run(t, model, cmd)
}

func connect(t *testing.T) (Model, *fakeClient) {
	t.Helper()
	client := newFakeClient(t)
	model := NewModel().WithClient(client)
	return run(t, model, model.Init()).(Model), client
}

func reply(t *testing.T, req protocol.Request, data any) eventMsg {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return eventMsg{protocol.Event{ID: "e", RequestID: req.ID, ClientID: "me", Subject: req.Subject, Recipient: req.Recipient, Kind: "result", Data: raw}}
}

func failure(req protocol.Request, text string) eventMsg {
	return eventMsg{protocol.Event{ID: "e", RequestID: req.ID, ClientID: "me", Subject: req.Subject, Recipient: req.Recipient, Kind: "error", Error: text}}
}

func isolateSettings(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	t.Setenv("AppData", root)
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "violet", "settings.json")
}

func assertNoSettingsFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("UI wrote settings itself (%v)", err)
	}
}

func plain(model tea.Model) string { return ansi.Strip(model.View().Content) }

func TestInitSubscribesThenRequestsGraphics(t *testing.T) {
	model, client := connect(t)
	sent := client.sent()
	if len(sent) != 1 || sent[0].Subject != protocol.SubjectGraphicsGet {
		t.Fatalf("expected one graphics.get, got %+v", sent)
	}
	if client.submitBeforeSubscribe {
		t.Fatal("submitted before subscribing")
	}
	if model.events == nil || model.graphicsRequest != sent[0].ID {
		t.Fatal("subscription or request id not recorded")
	}
}

func TestGraphicsReplyAppliesSettings(t *testing.T) {
	model, client := connect(t)
	settings := *meta.NewGraphicSettings()
	custom := components.Palette{Primary: color.White, Secondary: color.White, Accent: color.Black, Background: color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}}
	settings.Palettes["Custom"] = custom
	settings.Palette = "Custom"
	settings.MouseEnabled = false

	stale := protocol.Request{ID: "00000000-0000-4000-8000-000000000000", Subject: protocol.SubjectGraphicsGet}
	updated := update(t, model, reply(t, stale, settings)).(Model)
	if updated.selected != "Violet" {
		t.Fatal("stale graphics reply was applied")
	}

	updated = update(t, model, reply(t, client.last(t), settings)).(Model)
	if updated.selected != "Custom" || updated.graphics.MouseEnabled {
		t.Fatal("graphics reply not applied")
	}
	if updated.themes[len(updated.themes)-1].Name != "Custom" {
		t.Fatal("custom palette missing from themes")
	}
	view := updated.View()
	if view.MouseMode != tea.MouseModeNone {
		t.Fatal("mouse setting not applied")
	}
	if r, g, b, _ := view.BackgroundColor.RGBA(); r>>8 != 0x12 || g>>8 != 0x34 || b>>8 != 0x56 {
		t.Fatal("welcome page not restyled")
	}
	opened := update(t, updated, components.CommandSubmittedMsg{Text: `\theme`}).(Model)
	if opened.page.(pages.ThemePicker).SelectedName() != "Custom" {
		t.Fatal("picker does not select server palette")
	}
}

func TestThemeSelectSubmitsWithoutWritingSettings(t *testing.T) {
	settingsFile := isolateSettings(t)
	model, client := connect(t)
	var m tea.Model = update(t, model, components.CommandSubmittedMsg{Text: `\theme`})
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(client.sent()) != 1 {
		t.Fatal("preview or cancel submitted a request")
	}
	m = update(t, m, components.CommandSubmittedMsg{Text: `\theme`})
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	req := client.last(t)
	if req.Subject != protocol.SubjectThemeSelect || string(req.Data) != `{"name":"Misty"}` {
		t.Fatalf("unexpected request %s %s", req.Subject, req.Data)
	}
	got := m.(Model)
	if got.selected != "Misty" || got.graphicsRequest != req.ID {
		t.Fatal("selection not applied optimistically")
	}
	if _, ok := got.page.(pages.Welcome); !ok {
		t.Fatal("selection did not restore welcome")
	}
	assertNoSettingsFile(t, settingsFile)
}

func TestPaletteCreateSubmits(t *testing.T) {
	settingsFile := isolateSettings(t)
	model, client := connect(t)
	var m tea.Model = update(t, model, components.CommandSubmittedMsg{Text: `\create-palette`})
	m = update(t, m, tea.PasteMsg{Content: "Custom"})
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	req := client.last(t)
	if req.Subject != protocol.SubjectPaletteCreate {
		t.Fatalf("unexpected subject %s", req.Subject)
	}
	var body struct {
		Name    string             `json:"name"`
		Palette components.Palette `json:"palette"`
	}
	if err := json.Unmarshal(req.Data, &body); err != nil || body.Name != "Custom" || body.Palette.Background == nil {
		t.Fatalf("bad payload %s: %v", req.Data, err)
	}
	got := m.(Model)
	if got.selected != "Custom" || len(got.themes) != 7 {
		t.Fatal("palette not applied optimistically")
	}
	assertNoSettingsFile(t, settingsFile)
}

func TestErrorReplyIsSurfaced(t *testing.T) {
	model, client := connect(t)
	var m tea.Model = update(t, model, tea.WindowSizeMsg{Width: 120, Height: 24})
	m = update(t, m, pages.ThemeSelectedMsg{Name: "Forest"})
	selectReq := client.last(t)
	m, cmd := m.Update(failure(selectReq, "disk full"))
	if _, ok := m.(Model).page.(pages.Welcome); !ok {
		t.Fatal("error left the page")
	}
	if !strings.Contains(m.(Model).status, "disk full") || !strings.Contains(plain(m), "disk full") {
		t.Fatalf("error not surfaced: %q", m.(Model).status)
	}
	m = run(t, m, cmd)
	resync := client.last(t)
	if resync.Subject != protocol.SubjectGraphicsGet || resync.ID == selectReq.ID {
		t.Fatal("rejected change did not resynchronize")
	}
	// The UI remains usable after the error.
	m = update(t, m, components.CommandSubmittedMsg{Text: `\theme`})
	if _, ok := m.(Model).page.(pages.ThemePicker); !ok {
		t.Fatal("UI unusable after error")
	}
}

func TestSubmitFailureIsSurfaced(t *testing.T) {
	model, client := connect(t)
	client.submitErr = protocol.ErrBusy
	m := update(t, model, pages.ThemeSelectedMsg{Name: "Forest"})
	if !strings.Contains(m.(Model).status, protocol.ErrBusy.Error()) {
		t.Fatal("submit failure hidden")
	}
	// The optimistic change never reached the server, so the UI resyncs once
	// (the failed graphics.get itself must not trigger another resync).
	sent := client.sent()
	if len(sent) < 2 || sent[len(sent)-2].Subject != protocol.SubjectThemeSelect || sent[len(sent)-1].Subject != protocol.SubjectGraphicsGet {
		t.Fatalf("failed submit did not resynchronize: %+v", sent)
	}
	count := len(sent)
	update(t, m, tea.KeyPressMsg{Code: 'x'})
	if len(client.sent()) != count {
		t.Fatal("resync failure caused a resubmit loop")
	}
}

func TestServerTextIsSanitized(t *testing.T) {
	if got := eventError(protocol.Event{Error: "bad\x1b[2Jthing\a"}).Error(); strings.ContainsAny(got, "\x1b\a") || got != "bad[2Jthing" {
		t.Fatalf("error not sanitized: %q", got)
	}
	data, err := json.Marshal([]protocol.CatalogModel{{ID: "a\x1b]0;x\a", Name: "n\x1b[31m", Description: "d\r\n"}})
	if err != nil {
		t.Fatal(err)
	}
	models, err := decodeModels(protocol.Event{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{models[0].ID, models[0].Name, models[0].Description} {
		if strings.ContainsFunc(field, unicode.IsControl) {
			t.Fatalf("model text not sanitized: %q", field)
		}
	}
}

func TestProvidersRequestsCatalog(t *testing.T) {
	model, client := connect(t)
	var m tea.Model = update(t, model, components.CommandSubmittedMsg{Text: `\providers`})
	if _, ok := m.(Model).page.(pages.Providers); !ok {
		t.Fatal("\\providers did not open the page")
	}
	first := client.last(t)
	if first.Subject != protocol.SubjectModels || first.Recipient != "cloudflare" {
		t.Fatalf("unexpected request %+v", first)
	}
	if !strings.Contains(plain(m), "Loading") {
		t.Fatal("page not loading")
	}

	m = update(t, m, tea.KeyPressMsg{Code: 'r'})
	second := client.last(t)
	if second.Subject != protocol.SubjectModels || second.ID == first.ID {
		t.Fatal("refresh did not submit a new request")
	}
	m = update(t, m, reply(t, first, []protocol.CatalogModel{{ID: "old", Name: "stale-model"}}))
	if strings.Contains(plain(m), "stale-model") || !strings.Contains(plain(m), "Loading") {
		t.Fatal("stale reply was applied")
	}
	m = update(t, m, reply(t, second, []protocol.CatalogModel{{ID: "b", Name: "zeta-model"}, {ID: "a", Name: "alpha-model"}}))
	view := plain(m)
	if !strings.Contains(view, "alpha-model") || !strings.Contains(view, "zeta-model") {
		t.Fatalf("models not loaded:\n%s", view)
	}

	m = update(t, m, tea.KeyPressMsg{Code: 'r'})
	third := client.last(t)
	m = update(t, m, failure(third, "catalog unavailable"))
	if !strings.Contains(plain(m), "catalog unavailable") {
		t.Fatal("catalog error not shown")
	}
	if _, ok := m.(Model).page.(pages.Providers); !ok {
		t.Fatal("error left the providers page")
	}

	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := m.(Model).page.(pages.Welcome); !ok {
		t.Fatal("esc did not restore the previous page")
	}
}

func TestProvidersBeforeSubscriptionWaits(t *testing.T) {
	client := newFakeClient(t)
	model := NewModel().WithClient(client)
	m := update(t, model, components.CommandSubmittedMsg{Text: `\providers`})
	if len(client.sent()) != 0 {
		t.Fatal("submitted before subscribing")
	}
	m = run(t, m, m.Init())
	sent := client.sent()
	if len(sent) != 2 || sent[1].Subject != protocol.SubjectModels || client.submitBeforeSubscribe {
		t.Fatalf("deferred catalog request not sent: %+v", sent)
	}
}

func TestProvidersRespectsMouseSetting(t *testing.T) {
	model, client := connect(t)
	settings := *meta.NewGraphicSettings()
	settings.MouseEnabled = false
	m := update(t, model, reply(t, client.last(t), settings))
	m = update(t, m, components.CommandSubmittedMsg{Text: `\providers`})
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("providers page enabled mouse")
	}
}

func TestOfflineMode(t *testing.T) {
	settingsFile := isolateSettings(t)
	var m tea.Model = NewModel()
	if cmd := m.Init(); cmd != nil {
		t.Fatal("offline model should not start client work")
	}
	m = update(t, m, components.CommandSubmittedMsg{Text: `\providers`})
	if !strings.Contains(plain(m), "offline") {
		t.Fatalf("offline state not shown:\n%s", plain(m))
	}
	m = update(t, m, tea.KeyPressMsg{Code: 'r'})
	if !strings.Contains(plain(m), "offline") {
		t.Fatal("refresh left offline state")
	}
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = update(t, m, components.CommandSubmittedMsg{Text: `\theme`})
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.(Model).selected != "Misty" {
		t.Fatal("offline theme selection failed")
	}
	if _, ok := m.(Model).page.(pages.Welcome); !ok {
		t.Fatal("selection did not restore welcome")
	}
	assertNoSettingsFile(t, settingsFile)
}

func TestSubscribeFailureRunsOffline(t *testing.T) {
	client := newFakeClient(t)
	client.subscribeErr = protocol.ErrStopped
	model := NewModel().WithClient(client)
	m := run(t, model, model.Init())
	if !m.(Model).disconnected || !strings.Contains(m.(Model).status, protocol.ErrStopped.Error()) {
		t.Fatal("subscribe failure not recorded")
	}
	m = update(t, m, components.CommandSubmittedMsg{Text: `\providers`})
	if len(client.sent()) != 0 || !strings.Contains(plain(m), protocol.ErrDisconnected.Error()) {
		t.Fatal("providers should show a disconnected error")
	}
}

func TestDisconnectIsRecordedWithoutQuitting(t *testing.T) {
	model, _ := connect(t)
	var m tea.Model = update(t, model, tea.WindowSizeMsg{Width: 120, Height: 24})
	m = update(t, m, components.CommandSubmittedMsg{Text: `\providers`})
	m, cmd := m.Update(disconnectedMsg{})
	if cmd != nil {
		t.Fatal("disconnect produced a command")
	}
	got := m.(Model)
	if !got.disconnected || got.events != nil {
		t.Fatal("disconnect not recorded")
	}
	if !strings.Contains(plain(m), protocol.ErrDisconnected.Error()) {
		t.Fatal("pending catalog request not failed")
	}
	m = update(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(plain(m), "disconnected") {
		t.Fatalf("disconnect not shown in status bar:\n%s", plain(m))
	}
}

func TestListenCmd(t *testing.T) {
	events := make(chan protocol.Event, 1)
	events <- protocol.Event{ID: "x", Subject: protocol.SubjectChat, Kind: "chat.saved"}
	if msg, ok := listenCmd(events)().(eventMsg); !ok || msg.event.ID != "x" {
		t.Fatal("event not delivered")
	}
	close(events)
	if _, ok := listenCmd(events)().(disconnectedMsg); !ok {
		t.Fatal("closed channel should report a disconnect")
	}
	// Shared chat events are accepted and re-arm the listener.
	model, _ := connect(t)
	if _, cmd := model.Update(eventMsg{protocol.Event{ID: "y", Subject: protocol.SubjectChat, Kind: "chat.completed"}}); cmd == nil {
		t.Fatal("listener not re-armed")
	}
}

// The TUI must only reach the server through protocol.Client.
func TestUIDoesNotImportServer(t *testing.T) {
	const module = "github.com/tristanisham/violet"
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var visit func(pkg string)
	visit = func(pkg string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(pkg, module), "/")))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if path == module+"/server" || strings.HasPrefix(path, module+"/server/") {
					t.Errorf("%s imports %s", filepath.Join(dir, name), path)
				}
				if strings.HasPrefix(path, module+"/") && !strings.HasSuffix(name, "_test.go") {
					visit(path)
				}
			}
		}
	}
	if err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		visit(module + "/ui" + strings.TrimPrefix("/"+filepath.ToSlash(path), "/."))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
