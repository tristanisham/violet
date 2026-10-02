// Package protocol defines the application messages shared by local and HTTP clients.
package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

type Subject string

const (
	SubjectUnknown       Subject = ""
	SubjectChat          Subject = "chat"
	SubjectModels        Subject = "models.list"
	SubjectGraphicsGet   Subject = "graphics.get"
	SubjectThemeSelect   Subject = "graphics.theme.select"
	SubjectPaletteCreate Subject = "graphics.palette.create"
)

var (
	ErrStopped      = errors.New("server is not running")
	ErrDisconnected = errors.New("client disconnected")
	ErrBusy         = errors.New("server work queue is full")
)

type Request struct {
	ID             string          `json:"id"`
	Subject        Subject         `json:"subject"`
	Recipient      string          `json:"recipient,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Data           json.RawMessage `json:"data"`
}

func NewRequest(subject Subject, recipient string, payload any) (Request, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Request{}, err
	}
	return Request{ID: uuid.NewString(), Subject: subject, Recipient: recipient, Data: data}, nil
}

func (r Request) Validate() error {
	if _, err := uuid.Parse(r.ID); err != nil {
		return fmt.Errorf("request id must be a UUID")
	}
	switch r.Subject {
	case SubjectChat, SubjectModels, SubjectGraphicsGet, SubjectThemeSelect, SubjectPaletteCreate:
	default:
		return fmt.Errorf("unsupported subject %q", r.Subject)
	}
	if len(r.Recipient) > 256 || len(r.ConversationID) > 256 {
		return fmt.Errorf("routing identifiers are too long")
	}
	if !json.Valid(r.Data) {
		return fmt.Errorf("request data must be valid JSON")
	}
	return nil
}

// ClientID is a target for private replies; an empty ClientID means a shared event.
// OriginClientID identifies the requester even on a shared event.
type Event struct {
	ID             string          `json:"id"`
	RequestID      string          `json:"request_id,omitempty"`
	ClientID       string          `json:"client_id,omitempty"`
	OriginClientID string          `json:"origin_client_id,omitempty"`
	Subject        Subject         `json:"subject"`
	Recipient      string          `json:"recipient,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Kind           string          `json:"kind"`
	Data           json.RawMessage `json:"data,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// Subscribe must be established before submitting work. Slow consumers are
// disconnected rather than allowed to stall the server or silently lose events.
type Client interface {
	Submit(context.Context, Request) error
	Subscribe(context.Context) (<-chan Event, error)
	Close() error
}

type CatalogModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Task        struct {
		Name string `json:"name"`
	} `json:"task"`
}

type ChatMessage struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}
type ChatInput struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}
