package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tristanisham/violet/meta"
)

func (s *Engine) StartHttp(settings *meta.Settings) error {
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", s.Port), s.httpRouter(settings))
}

func (s *Engine) httpRouter(settings *meta.Settings) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Post("/api/start", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Start(settings); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeStatus(w)
	})
	r.Post("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		s.Stop()
		s.writeStatus(w)
	})
	r.Get("/api/status", func(w http.ResponseWriter, r *http.Request) {
		s.writeStatus(w)
	})
	return r
}

func (s *Engine) writeStatus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Running bool `json:"running"`
	}{Running: s.Status()})
}
