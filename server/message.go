package server

type Subject int

const (
	SubjectUnknown Subject = iota
)

type Message struct {
	Subject Subject
}
