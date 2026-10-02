package pages

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/ui/components"
)

func themePickerChoices() []ThemeChoice {
	return []ThemeChoice{
		{Name: "Dawn", Styles: components.NewStyles(color.White, color.White, color.RGBA{R: 180, A: 255}, color.RGBA{R: 30, A: 255})},
		{Name: "Ocean", Styles: components.NewStyles(color.Black, color.Black, color.RGBA{B: 180, A: 255}, color.RGBA{B: 30, A: 255})},
		{Name: "Forest", Styles: components.NewStyles(color.White, color.White, color.RGBA{G: 180, A: 255}, color.RGBA{G: 30, A: 255})},
	}
}

func TestThemePickerNavigationAndPreview(t *testing.T) {
	choices := themePickerChoices()
	m := NewThemePicker(choices, "Ocean")
	if m.SelectedName() != "Ocean" || m.Init() != nil {
		t.Fatal("initial selection or Init is incorrect")
	}
	for _, step := range []struct {
		key  tea.KeyPressMsg
		name string
	}{
		{tea.KeyPressMsg{Code: 'j'}, "Forest"},
		{tea.KeyPressMsg{Code: 'k'}, "Ocean"},
		{tea.KeyPressMsg{Code: tea.KeyUp}, "Dawn"},
		{tea.KeyPressMsg{Code: tea.KeyDown}, "Ocean"},
	} {
		before := m.View().Content
		updated, _ := m.Update(step.key)
		m = updated.(ThemePicker)
		if m.SelectedName() != step.name {
			t.Fatalf("%s selected %q, want %q", step.key.String(), m.SelectedName(), step.name)
		}
		var expected components.Styles
		for _, choice := range choices {
			if choice.Name == step.name {
				expected = choice.Styles
			}
		}
		view := m.View()
		if view.BackgroundColor != expected.Background || !view.AltScreen {
			t.Fatal("preview did not update full-terminal background")
		}
		if view.Content == before {
			t.Fatal("preview did not repaint")
		}
		for _, choice := range choices {
			if !strings.Contains(ansi.Strip(view.Content), choice.Name) {
				t.Fatalf("theme %s is missing from preview", choice.Name)
			}
		}
		status := components.StatusBar(m.width, "Theme · "+m.SelectedName(), "j/k or ↑/↓ · enter: select · esc: cancel ", expected)
		if !strings.HasSuffix(view.Content, status) {
			t.Fatal("status bar did not adopt preview styles")
		}
	}
	if NewThemePicker(choices, "missing").SelectedName() != "Dawn" {
		t.Fatal("unknown selection should fall back to first choice")
	}
}

func TestThemePickerMessages(t *testing.T) {
	m := NewThemePicker(themePickerChoices(), "Ocean")
	for _, key := range []tea.KeyPressMsg{{Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}} {
		if _, cmd := m.Update(key); cmd != nil {
			t.Fatal("q and ctrl+c should not quit")
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if selected, ok := cmd().(ThemeSelectedMsg); !ok || selected.Name != "Ocean" {
		t.Fatal("enter should select current theme")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(ThemeCancelledMsg); !ok {
		t.Fatal("esc should cancel")
	}
}

func TestThemePickerLayout(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 8, Height: 4}, {Width: 1, Height: 1}, {Width: 0, Height: 0}} {
		m, _ := NewThemePicker(themePickerChoices(), "Dawn").Update(size)
		content := m.View().Content
		if size.Width == 0 || size.Height == 0 {
			if content != "" {
				t.Fatal("zero-size view should be empty")
			}
			continue
		}
		if lipgloss.Width(content) > size.Width || lipgloss.Height(content) != size.Height {
			t.Fatalf("viewport %dx%d, content %dx%d", size.Width, size.Height, lipgloss.Width(content), lipgloss.Height(content))
		}
	}
}
