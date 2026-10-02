package ai

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ChatMessage struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type ChatRequest struct {
	Id        uuid.UUID     `json:"id" gorm:"type:text;primaryKey"`
	Recipient string        `json:"recipient"`
	Messages  []ChatMessage `json:"messages" gorm:"serializer:json;type:text"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (r *ChatRequest) BeforeCreate(tx *gorm.DB) error {
	if r.Id == uuid.Nil {
		r.Id = uuid.New()
	}
	if r.Recipient == "" {
		r.Recipient = DefaultModel
	}
	return nil
}

func (r ChatRequest) Subject() Subject {
	return SubjectChat
}

func (r ChatRequest) Recipiant() string {
	if r.Recipient == "" {
		return DefaultModel
	}
	return r.Recipient
}

func (r ChatRequest) Content() any {
	return r
}
