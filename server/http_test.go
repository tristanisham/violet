package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
)

func TestHTTPEngineLifecycle(t *testing.T) {
	s := NewEngine()
	defer s.Stop()
	router := s.httpRouter(&meta.State{Config: &meta.Config{ContainerSock: "/tmp/container.sock", ProjectDir: t.TempDir()}})
	for _, step := range []struct {
		method  string
		path    string
		running bool
	}{
		{http.MethodGet, "/api/status", false},
		{http.MethodPost, "/api/stop", false},
		{http.MethodPost, "/api/start", true},
		{http.MethodPost, "/api/start", true},
		{http.MethodGet, "/api/status", true},
		{http.MethodPost, "/api/stop", false},
		{http.MethodPost, "/api/stop", false},
		{http.MethodPost, "/api/start", true},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, loopbackRequest(step.method, step.path))
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: status %d, body %s", step.method, step.path, w.Code, w.Body.String())
		}
		var response struct {
			Running bool `json:"running"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Running != step.running {
			t.Fatalf("%s %s: running = %v, want %v", step.method, step.path, response.Running, step.running)
		}
	}
}

func TestHTTPStartMissingState(t *testing.T) {
	for _, state := range []*meta.State{nil, {}, {Config: &meta.Config{}}} {
		s := NewEngine()
		w := httptest.NewRecorder()
		s.httpRouter(state).ServeHTTP(w, loopbackRequest(http.MethodPost, "/api/start"))
		if w.Code != http.StatusBadRequest || s.Status() {
			t.Fatalf("invalid state: status %d, running %v", w.Code, s.Status())
		}
	}
}

func TestConcurrentEngineLifecycle(t *testing.T) {
	s := NewEngine()
	state := &meta.State{Config: &meta.Config{ContainerSock: "/tmp/container.sock", ProjectDir: t.TempDir()}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := s.Start(state); err != nil {
				t.Error(err)
			}
			s.Status()
			s.Stop()
		})
	}
	wg.Wait()
	s.Stop()
	if s.Status() {
		t.Fatal("engine is still running")
	}
}

func newHTTPTestServer(t *testing.T) (*Engine, *httptest.Server) {
	t.Helper()
	engine, _ := newStartedEngine(t)
	ts := httptest.NewServer(engine.Handler(newTestState(t)))
	t.Cleanup(ts.Close)
	t.Cleanup(engine.Stop)
	return engine, ts
}

func doRaw(t *testing.T, method, url string, body string, headers map[string]string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func validRequestJSON(t *testing.T) string {
	t.Helper()
	req, err := protocol.NewRequest(protocol.SubjectModels, "", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHTTPMessageRequestValidation(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	id := uuid.NewString()
	valid := validRequestJSON(t)
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		headers map[string]string
		status  int
	}{
		{"origin on messages", http.MethodPost, "/api/messages", valid, map[string]string{"X-Violet-Client-ID": id, "Origin": "https://evil.example"}, http.StatusForbidden},
		{"origin on events", http.MethodGet, "/api/events", "", map[string]string{"X-Violet-Client-ID": id, "Origin": "null"}, http.StatusForbidden},
		{"missing client id", http.MethodPost, "/api/messages", valid, nil, http.StatusBadRequest},
		{"bad client id", http.MethodPost, "/api/messages", valid, map[string]string{"X-Violet-Client-ID": "not-a-uuid"}, http.StatusBadRequest},
		{"bad client id on events", http.MethodGet, "/api/events", "", map[string]string{"X-Violet-Client-ID": "nope"}, http.StatusBadRequest},
		{"malformed json", http.MethodPost, "/api/messages", "{not json", map[string]string{"X-Violet-Client-ID": id}, http.StatusBadRequest},
		{"unknown field", http.MethodPost, "/api/messages", `{"id":"x","bogus":1}`, map[string]string{"X-Violet-Client-ID": id}, http.StatusBadRequest},
		{"two requests", http.MethodPost, "/api/messages", valid + valid, map[string]string{"X-Violet-Client-ID": id}, http.StatusBadRequest},
		{"oversize", http.MethodPost, "/api/messages", `{"id":"` + strings.Repeat("a", maxHTTPMessageBytes) + `"}`, map[string]string{"X-Violet-Client-ID": id}, http.StatusRequestEntityTooLarge},
		{"unsubscribed", http.MethodPost, "/api/messages", valid, map[string]string{"X-Violet-Client-ID": id}, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if resp := doRaw(t, tc.method, ts.URL+tc.path, tc.body, tc.headers); resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
		})
	}
}

func TestHTTPEventStreamAndStoppedEngine(t *testing.T) {
	engine, ts := newHTTPTestServer(t)
	id := uuid.NewString()
	resp := doRaw(t, http.MethodGet, ts.URL+"/api/events", "", map[string]string{"X-Violet-Client-ID": id})
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// Reconnecting with the same client ID replaces the old stream, which ends.
	again := doRaw(t, http.MethodGet, ts.URL+"/api/events", "", map[string]string{"X-Violet-Client-ID": id})
	if again.StatusCode != http.StatusOK {
		t.Fatalf("replacement stream status = %d", again.StatusCode)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("replaced stream did not end cleanly: %v", err)
	}
	resp = again
	if post := doRaw(t, http.MethodPost, ts.URL+"/api/messages", validRequestJSON(t), map[string]string{"X-Violet-Client-ID": id}); post.StatusCode != http.StatusAccepted {
		t.Fatalf("subscribed submit status = %d", post.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "data: ") {
		t.Fatalf("first frame = %q, %v", line, err)
	}
	var event protocol.Event
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil || event.ClientID != id {
		t.Fatalf("event = %+v, %v", event, err)
	}
	engine.Stop()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("stream did not end cleanly: %v", err)
	}
	other := uuid.NewString()
	if r := doRaw(t, http.MethodGet, ts.URL+"/api/events", "", map[string]string{"X-Violet-Client-ID": other}); r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stopped events status = %d", r.StatusCode)
	}
	if r := doRaw(t, http.MethodPost, ts.URL+"/api/messages", validRequestJSON(t), map[string]string{"X-Violet-Client-ID": other}); r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stopped submit status = %d", r.StatusCode)
	}
}

func TestHTTPBearerToken(t *testing.T) {
	t.Setenv("VIOLET_SERVER_TOKEN", "s3cret")
	_, ts := newHTTPTestServer(t)
	for _, auth := range []string{"", "Bearer wrong", "s3cret", "Bearer s3cretX"} {
		headers := map[string]string{}
		if auth != "" {
			headers["Authorization"] = auth
		}
		resp := doRaw(t, http.MethodGet, ts.URL+"/api/status", "", headers)
		if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("auth %q: status %d", auth, resp.StatusCode)
		}
	}
	if resp := doRaw(t, http.MethodGet, ts.URL+"/api/status", "", map[string]string{"Authorization": "Bearer s3cret"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("valid token status = %d", resp.StatusCode)
	}
}

func TestResolveListenAddr(t *testing.T) {
	for _, tc := range []struct {
		addr, token, want string
		ok                bool
	}{
		{"", "", "127.0.0.1:8080", true},
		{"127.0.0.1:9000", "", "127.0.0.1:9000", true},
		{"localhost:9000", "", "localhost:9000", true},
		{"[::1]:9000", "", "[::1]:9000", true},
		{"127.5.5.5:9000", "", "127.5.5.5:9000", true},
		{"0.0.0.0:9000", "", "", false},
		{":9000", "", "", false},
		{"192.168.1.10:9000", "", "", false},
		{"[::]:9000", "", "", false},
		{"0.0.0.0:9000", "tok", "0.0.0.0:9000", true},
		{"no-port", "", "", false},
	} {
		got, err := resolveListenAddr(tc.addr, 8080, tc.token)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("resolveListenAddr(%q, token=%v) = %q, %v", tc.addr, tc.token != "", got, err)
		}
	}
	engine := NewEngine()
	engine.Addr = "0.0.0.0:0"
	t.Setenv("VIOLET_SERVER_TOKEN", "")
	if err := engine.StartHttp(newTestState(t)); err == nil || !strings.Contains(err.Error(), "VIOLET_SERVER_TOKEN") {
		t.Fatalf("StartHttp on public address without token = %v", err)
	}
}

// loopbackRequest builds a recorder request whose Host passes the
// DNS-rebinding guard (httptest defaults to example.com).
func loopbackRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Host = "127.0.0.1:8080"
	return req
}

func TestHTTPRejectsBrowsersOnEveryRoute(t *testing.T) {
	_, ts := newHTTPTestServer(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/start"}, {http.MethodPost, "/api/stop"}, {http.MethodGet, "/api/status"},
		{http.MethodPost, "/api/messages"}, {http.MethodGet, "/api/events"},
	} {
		if resp := doRaw(t, route.method, ts.URL+route.path, "", map[string]string{"Origin": "https://evil.example"}); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with Origin = %d, want 403", route.method, route.path, resp.StatusCode)
		}
	}
	engine := NewEngine()
	for host, want := range map[string]int{
		"evil.example:8080": http.StatusForbidden, "attacker.test": http.StatusForbidden,
		"127.0.0.1:8080": http.StatusOK, "localhost": http.StatusOK, "[::1]:8080": http.StatusOK,
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.Host = host
		engine.Handler(nil).ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("Host %q = %d, want %d", host, w.Code, want)
		}
	}
	// With a token the server may be remote, so arbitrary hosts are allowed.
	t.Setenv("VIOLET_SERVER_TOKEN", "secret")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	engine.Handler(nil).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated remote host = %d", w.Code)
	}
}
