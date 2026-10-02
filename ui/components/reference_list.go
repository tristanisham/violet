package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Reference struct {
	Name        string
	Description string
}

func ReferenceList(title string, rows []Reference, width int, styles Styles) string {
	if width <= 0 {
		return ""
	}
	nameWidth := 0
	for _, row := range rows {
		nameWidth = max(nameWidth, lipgloss.Width(row.Name))
	}
	nameWidth = min(nameWidth, max(1, width/2))
	descriptionWidth := max(0, width-nameWidth-1)
	lines := []string{styles.Title.Width(width).MaxWidth(width).Render(title)}
	for _, row := range rows {
		name := styles.Title.Width(nameWidth).Render(ansi.Truncate(row.Name, nameWidth, ""))
		description := styles.Text.Width(descriptionWidth).Align(lipgloss.Right).Render(ansi.Truncate(row.Description, descriptionWidth, ""))
		lines = append(lines, styles.Canvas.MaxWidth(width).MaxHeight(1).Render(name+" "+description))
	}
	return strings.Join(lines, "\n")
}
