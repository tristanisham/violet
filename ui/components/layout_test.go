package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestReferenceListFitsWidth(t *testing.T) {
	p := VioletPalette
	styles := NewStyles(p.Primary, p.Secondary, p.Accent, p.Background)
	for width := 1; width <= 20; width++ {
		view := ReferenceList("Commands", []Reference{{Name: "界界", Description: "description"}}, width, styles)
		for _, line := range strings.Split(view, "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("width %d: rendered %d columns: %q", width, got, line)
			}
		}
	}
}

func TestStatusBarExactFit(t *testing.T) {
	p := VioletPalette
	styles := NewStyles(p.Primary, p.Secondary, p.Accent, p.Background)
	if got := ansi.Strip(StatusBar(6, "ab", "cd", styles)); got != " ab cd" {
		t.Fatalf("exact fit dropped text: %q", got)
	}
}
