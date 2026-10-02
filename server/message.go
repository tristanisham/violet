package server

import "github.com/tristanisham/violet/server/ai"

type Message interface {
	Subject() ai.Subject
	Recipiant() string
	Content() any
}

var _ Message = ai.ChatRequest{}
