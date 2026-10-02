package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
)

const (
	maxHTTPMessageBytes = 1 << 20
	sseWriteTimeout     = 10 * time.Second
	maxHTTPBodyReadTime = 15 * time.Second
)

func (s *Engine) StartHttp(state *meta.State) error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(context.Background(), ln, state)
}

// Listen validates and binds the HTTP address without serving, so callers can
// reject a bad --listen value before starting the engine.
func (s *Engine) Listen() (net.Listener, error) {
	addr, err := resolveListenAddr(s.Addr, s.Port, os.Getenv("VIOLET_SERVER_TOKEN"))
	if err != nil {
		return nil, err
	}
	return net.Listen("tcp", addr)
}

// Serve serves the API on ln until ctx is done, then shuts down: in-flight
// requests get a grace period, after which long-lived event streams are closed.
func (s *Engine) Serve(ctx context.Context, ln net.Listener, state *meta.State) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // releases the shutdown watcher if Serve fails on its own
	server := &http.Server{
		Handler:           s.Handler(state),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		// Event streams only end when their subscription closes; close them so
		// Shutdown completes promptly. The engine itself keeps running.
		s.disconnectClients()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
		}
	}()
	err := server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		return nil
	}
	return err
}

// resolveListenAddr returns the address StartHttp binds. An empty addr means
// loopback on port. Non-loopback hosts (including the empty host, which binds
// every interface) are refused unless a bearer token is configured.
func resolveListenAddr(addr string, port uint16, token string) (string, error) {
	if strings.TrimSpace(addr) == "" {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if !isLoopbackHost(host) && token == "" {
		return "", fmt.Errorf("refusing to listen on non-loopback address %q without VIOLET_SERVER_TOKEN; shared servers must be authenticated", addr)
	}
	return addr, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Handler exposes the control and message APIs. StartHttp binds to loopback unless
// Addr is set; non-loopback listeners require VIOLET_SERVER_TOKEN, and callers
// exposing Handler remotely should also terminate TLS in front of it.
func (s *Engine) Handler(state *meta.State) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	token := os.Getenv("VIOLET_SERVER_TOKEN")
	r.Use(rejectBrowsers(token))
	expected := sha256.Sum256([]byte("Bearer " + token))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := sha256.Sum256([]byte(r.Header.Get("Authorization")))
			if token != "" && (subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 || len(r.Header.Values("Authorization")) != 1) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Post("/api/start", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Start(state); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeStatus(w)
	})
	r.Post("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		s.Stop()
		s.writeStatus(w)
	})
	r.Get("/api/status", func(w http.ResponseWriter, r *http.Request) { s.writeStatus(w) })
	r.Post("/api/messages", s.postMessage)
	r.Get("/api/events", s.streamEvents)
	return r
}

func (s *Engine) httpRouter(state *meta.State) http.Handler { return s.Handler(state) }

// rejectBrowsers protects every route from cross-site requests. Browsers attach
// Origin to cross-origin POSTs, so any Origin is refused. Without a token the
// API is loopback-only, so a non-loopback Host means DNS rebinding and is
// refused too; same-origin rebinding GETs carry no Origin header.
func rejectBrowsers(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(r.Header.Values("Origin")) != 0 {
				http.Error(w, "browser origins are not allowed", http.StatusForbidden)
				return
			}
			if token == "" {
				host := r.Host
				if h, _, err := net.SplitHostPort(host); err == nil {
					host = h
				}
				if !isLoopbackHost(strings.Trim(host, "[]")) {
					http.Error(w, "host not allowed", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func httpClientID(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.Header.Values("X-Violet-Client-ID")
	if len(values) != 1 {
		http.Error(w, "X-Violet-Client-ID must be a UUID", http.StatusBadRequest)
		return "", false
	}
	id, err := uuid.Parse(values[0])
	if err != nil {
		http.Error(w, "X-Violet-Client-ID must be a UUID", http.StatusBadRequest)
		return "", false
	}
	return id.String(), true
}

func writeTransportError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, protocol.ErrBusy):
		status = http.StatusTooManyRequests
	case errors.Is(err, protocol.ErrStopped):
		status = http.StatusServiceUnavailable
	case errors.Is(err, protocol.ErrDisconnected):
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}

func (s *Engine) postMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := httpClientID(w, r)
	if !ok {
		return
	}
	// Bound slow request bodies; MaxBytesReader only bounds their size.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(maxHTTPBodyReadTime))
	r.Body = http.MaxBytesReader(w, r.Body, maxHTTPMessageBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req protocol.Request
	err := decoder.Decode(&req)
	if err == nil {
		var extra any
		if err = decoder.Decode(&extra); err == io.EOF {
			err = req.Validate()
		} else if err == nil {
			err = fmt.Errorf("expected one JSON request")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body exceeds 1 MiB", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	if err := r.Context().Err(); err != nil {
		writeTransportError(w, err)
		return
	}
	// Accepted work belongs to the engine, not the short-lived POST connection.
	// The engine combines this context with its shutdown context in execute.
	if err := s.Submit(context.WithoutCancel(r.Context()), id, req); err != nil {
		writeTransportError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(struct {
		ID string `json:"id"`
	}{ID: req.ID})
}

func (s *Engine) streamEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := httpClientID(w, r)
	if !ok {
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
		http.Error(w, "streaming write deadlines unavailable", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events, err := s.Subscribe(ctx, id)
	if err != nil {
		writeTransportError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := controller.Flush(); err != nil {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		var frame []byte
		select {
		case <-ctx.Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				return
			}
			frame = append(append([]byte("data: "), data...), '\n', '\n')
		case <-heartbeat.C:
			frame = []byte(": keepalive\n\n")
		}
		if err := controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
			return
		}
		if _, err := w.Write(frame); err != nil {
			return
		}
		if err := controller.Flush(); err != nil {
			return
		}
	}
}

func (s *Engine) writeStatus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Running bool `json:"running"`
	}{Running: s.Status()})
}
