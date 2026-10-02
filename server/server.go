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
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Start(settings *meta.Settings) error {
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	s.ContainerSock = settings.Config.ContainerSock
	if s.ContainerSock == "" {
		return meta.ErrMissingSock
	}

	return http.ListenAndServe(fmt.Sprintf(":%d", s.Port), r)
}
