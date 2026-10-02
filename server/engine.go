package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/server/ai"
)

type Engine struct {
	Port uint16
	// Addr is the HTTP listen address. Empty means 127.0.0.1:<Port>. Non-loopback
	// addresses require VIOLET_SERVER_TOKEN so shared servers are authenticated.
	Addr          string
	ContainerSock string
	Router        chan Message

	// Configure these hooks before Start; each run snapshots them.
	LoadModels func(context.Context) ([]protocol.CatalogModel, error)
	RunChat    func(context.Context, string, ai.ChatRequest) (json.RawMessage, error)

	// Lifecycle operations serialize separately so shutdown never locks out clients.
	lifecycleMu sync.Mutex
	mu          sync.Mutex
	runtime     *engineRuntime
}

func NewEngine() *Engine {
	return &Engine{
		Port: 8080, Router: make(chan Message, requestQueueSize),
		LoadModels: loadModels, RunChat: runChat,
	}
}

func (s *Engine) Start(state *meta.State) error {
	if state == nil || state.Config == nil {
		return meta.ErrStaleState
	}
	if strings.TrimSpace(state.Config.ProjectDir) == "" {
		return fmt.Errorf("project_dir must not be empty")
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtime != nil {
		return nil
	}
	// Zero-value settings mean "none loaded" and get defaults. Loaded but invalid
	// settings are an error: silently substituting defaults would let the next
	// theme change overwrite the user's settings.json and lose custom palettes.
	graphics := *meta.NewGraphicSettings()
	if state.GraphicSettings.Palettes != nil {
		snapshot, err := copyGraphics(state.GraphicSettings)
		if err != nil {
			return fmt.Errorf("invalid graphic settings: %w", err)
		}
		graphics = snapshot
	}
	store, err := ai.OpenStore(state.Config.ProjectDir)
	if err != nil {
		return fmt.Errorf("open chat storage: %w", err)
	}
	if s.Router == nil {
		s.Router = make(chan Message, requestQueueSize)
	}
	r := newEngineRuntime(store, graphics, s.LoadModels, s.RunChat)
	s.ContainerSock = state.Config.ContainerSock
	s.runtime = r
	go r.run(s.Router)
	return nil
}

func (s *Engine) Stop() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	r := s.runtime
	if r != nil {
		r.cancel()
	}
	s.mu.Unlock()
	if r == nil {
		return
	}
	<-r.done
	s.mu.Lock()
	s.runtime = nil
	s.mu.Unlock()
}

func (s *Engine) Status() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtime != nil && s.runtime.ctx.Err() == nil
}

func loadModels(ctx context.Context) ([]protocol.CatalogModel, error) {
	gateway, err := ai.NewAiGateway()
	if err != nil {
		return nil, err
	}
	models, err := gateway.ListTextGenerationModels(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]protocol.CatalogModel, len(models))
	for i, model := range models {
		result[i] = protocol.CatalogModel(model)
	}
	return result, nil
}

func runChat(ctx context.Context, model string, chat ai.ChatRequest) (json.RawMessage, error) {
	gateway, err := ai.NewAiGateway()
	if err != nil {
		return nil, err
	}
	response, err := gateway.Run(ctx, model, chat)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read chat response: %w", err)
	}
	if len(data) > limit {
		return nil, fmt.Errorf("chat response exceeds %d bytes", limit)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("chat response is not valid JSON")
	}
	return json.RawMessage(data), nil
}
