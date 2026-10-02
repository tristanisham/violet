package server

import (
	"context"
	"fmt"
	"sync"

	"charm.land/log/v2"

	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/server/ai"
)

type Engine struct {
	Port          uint16
	ContainerSock string
	Router        chan Message
	mu            sync.Mutex
	quit          chan struct{}
	done          chan struct{}
}

func NewEngine() *Engine {
	return &Engine{
		Port:   8080,
		Router: make(chan Message),
	}
}

func (s *Engine) Start(settings *meta.Settings) error {
	if settings == nil || settings.Config == nil {
		return meta.ErrStaleSettings
	}
	if settings.Config.ContainerSock == "" {
		return meta.ErrMissingSock
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running() {
		return nil
	}

	store, err := ai.OpenStore(settings.Config.ProjectDir)
	if err != nil {
		return fmt.Errorf("open chat storage: %w", err)
	}

	s.ContainerSock = settings.Config.ContainerSock
	s.quit = make(chan struct{})
	s.done = make(chan struct{})
	go s.run(s.quit, s.done, store)
	return nil
}

func (s *Engine) run(quit <-chan struct{}, done chan<- struct{}, store *ai.Store) {
	defer close(done)
	defer func() {
		if err := store.Close(); err != nil {
			log.Error("close chat storage", "error", err)
		}
	}()
	for {
		select {
		case <-quit:
			return
		case message, ok := <-s.Router:
			if !ok {
				return
			}
			if err := saveMessage(store, message); err != nil {
				log.Error("store chat message", "error", err)
			}
		}
	}
}

func saveMessage(store *ai.Store, message Message) error {
	if message == nil || message.Subject() != ai.SubjectChat {
		return nil
	}
	chat, ok := message.Content().(ai.ChatRequest)
	if !ok {
		return fmt.Errorf("chat message has invalid content")
	}
	chat.Recipient = message.Recipiant()
	return store.Save(context.Background(), &chat)
}

func (s *Engine) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running() {
		return
	}

	close(s.quit)
	<-s.done
}

func (s *Engine) Status() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running()
}

// running is called with mu held.
func (s *Engine) running() bool {
	if s.done == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
