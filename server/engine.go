package server

import (
	"sync"

	"github.com/tristanisham/violet/meta"
)

type Engine struct {
	Port          uint16
	ContainerSock string
	In            chan Message
	mu            sync.Mutex
	quit          chan struct{}
	done          chan struct{}
}

func NewServer() *Engine {
	return &Engine{
		Port: 8080,
		In:   make(chan Message),
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

	s.ContainerSock = settings.Config.ContainerSock
	s.quit = make(chan struct{})
	s.done = make(chan struct{})
	go s.run(s.quit, s.done)
	return nil
}

func (s *Engine) run(quit <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-quit:
			return
		case message, ok := <-s.In:
			if !ok {
				return
			}
			_ = message
		}
	}
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
