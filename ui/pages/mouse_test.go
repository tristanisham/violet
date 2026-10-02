package pages

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestWelcomeMouseFocus(t *testing.T) {
	m := testWelcome()
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse reporting is not enabled")
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: 10, Y: 22, Button: tea.MouseLeft})
	if !updated.(Welcome).command.Focused() {
		t.Fatal("command-bar click should focus input")
	}
	for _, click := range []tea.MouseClickMsg{
		{X: 10, Y: 23, Button: tea.MouseLeft},
		{X: 10, Y: 22, Button: tea.MouseRight},
		{X: -1, Y: 22, Button: tea.MouseLeft},
	} {
		updated, _ = m.Update(click)
		if updated.(Welcome).command.Focused() {
			t.Fatal("outside/right-button clicks should not focus input")
		}
	}
}

func TestThemePickerMouse(t *testing.T) {
	choices := themePickerChoices()
	m := NewThemePicker(choices, "Dawn")
	view := m.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse reporting is not enabled")
	}
	found := false
	for y, line := range strings.Split(ansi.Strip(view.Content), "\n") {
		index := strings.Index(line, "Forest")
		if index < 0 {
			continue
		}
		found = true
		updated, cmd := m.Update(tea.MouseClickMsg{X: lipgloss.Width(line[:index]), Y: y, Button: tea.MouseLeft})
		m = updated.(ThemePicker)
		if m.SelectedName() != "Forest" || cmd != nil || m.View().BackgroundColor != choices[2].Styles.Background {
			t.Fatal("click should preview the selected theme without confirming")
		}
		break
	}
	if !found {
		t.Fatal("theme row not rendered")
	}
	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m = updated.(ThemePicker)
	if m.SelectedName() != "Ocean" {
		t.Fatal("wheel up should move selection up")
	}
	updated, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m = updated.(ThemePicker)
	if m.SelectedName() != "Forest" {
		t.Fatal("wheel down should move selection down")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if updated.(ThemePicker).SelectedName() != "Forest" {
		t.Fatal("click outside theme rows should not change selection")
	}
}
