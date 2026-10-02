package server

import (
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/server/ai"
)

type Message interface {
	Subject() protocol.Subject
	Recipiant() string
	Content() any
}

var _ Message = ai.ChatRequest{}
