package ai

import "github.com/tristanisham/violet/protocol"

type Subject = protocol.Subject

const (
	SubjectUnknown       = protocol.SubjectUnknown
	SubjectChat          = protocol.SubjectChat
	SubjectModels        = protocol.SubjectModels
	SubjectGraphicsGet   = protocol.SubjectGraphicsGet
	SubjectThemeSelect   = protocol.SubjectThemeSelect
	SubjectPaletteCreate = protocol.SubjectPaletteCreate
)
