package components

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type Styles struct {
	Title      lipgloss.Style
	Text       lipgloss.Style
	Box        lipgloss.Style
	Canvas     lipgloss.Style
	Status     lipgloss.Style
	Background color.Color
}

func NewStyles(primary, secondary, accent, background color.Color) Styles {
	return Styles{
		Title:      lipgloss.NewStyle().Foreground(primary).Background(background).Bold(true),
		Text:       lipgloss.NewStyle().Foreground(secondary).Background(background),
		Box:        lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).BorderBackground(background).Background(background).Padding(1, 2),
		Canvas:     lipgloss.NewStyle().Background(background),
		Status:     lipgloss.NewStyle().Foreground(secondary).Background(accent),
		Background: background,
	}
}
