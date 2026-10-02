package server

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tristanisham/violet/meta"
)

type Server struct {
	Port          uint16
	ContainerSock string
	In            chan Message
	quit          chan struct{}
}

func NewServer() *Server {
	return &Server{
		In: make(chan Message),
	}
}

func (s *Server) Start(settings *meta.Settings) error {
	s.ContainerSock = settings.Config.ContainerSock
	if s.ContainerSock == "" {
		return meta.ErrMissingSock
	}

	s.quit = make(chan struct{})

	go func() {
		for {
			select {
			case <-s.quit:
				return
			case message := <-s.In:
				_ = message
			}
		}
	}()

	return nil
}

func (s *Server) Stop() {
	s.quit <- struct{}{}
}

func (s *Server) StartHttp() error {
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	return http.ListenAndServe(fmt.Sprintf(":%d", s.Port), r)
}
