package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/tristanisham/violet/meta"
)

func TestHTTPEngineLifecycle(t *testing.T) {
	s := NewEngine()
	defer s.Stop()
	router := s.httpRouter(&meta.Settings{Config: &meta.Config{ContainerSock: "/tmp/container.sock", ProjectDir: t.TempDir()}})
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
		router.ServeHTTP(w, httptest.NewRequest(step.method, step.path, nil))
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

func TestHTTPStartMissingSettings(t *testing.T) {
	for _, settings := range []*meta.Settings{nil, {}, {Config: &meta.Config{}}} {
		s := NewEngine()
		w := httptest.NewRecorder()
		s.httpRouter(settings).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/start", nil))
		if w.Code != http.StatusBadRequest || s.Status() {
			t.Fatalf("invalid settings: status %d, running %v", w.Code, s.Status())
		}
	}
}

func TestConcurrentEngineLifecycle(t *testing.T) {
	s := NewEngine()
	settings := &meta.Settings{Config: &meta.Config{ContainerSock: "/tmp/container.sock", ProjectDir: t.TempDir()}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := s.Start(settings); err != nil {
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
