package server

import "uuid"

type ChatMessage struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type ChatRequest struct {
	Id        uuid.UUID `json:"id"`
	Messages  []ChatMessage `json:"messages"`
}

func (r ChatRequest) Subject() Subject {
	return SubjectChat
}

func (r ChatRequest) Content() any {
	return r
}


