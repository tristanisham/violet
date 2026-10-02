package server

type Subject int

const (
	SubjectUnknown Subject = iota
	SubjectChat
)

type Message interface {
	Subject() Subject
	Content() any
}
