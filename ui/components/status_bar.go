package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func StatusBar(width int, left, right string, styles Styles) string {
	if width <= 0 {
		return ""
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	content := " " + left + " "
	if gap > 0 {
		content += strings.Repeat(" ", gap) + right
	}
	return styles.Status.Width(width).MaxHeight(1).Render(ansi.Truncate(content, width, ""))
}
