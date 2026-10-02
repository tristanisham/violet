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

func testWelcome() Welcome {
	return NewWelcome(components.NewStyles(color.White, color.White, color.Black, color.Black))
}

func TestWelcomeLayout(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{
		{Width: 80, Height: 24},
		{Width: 120, Height: 40},
		{Width: 32, Height: 10},
		{Width: 8, Height: 4},
		{Width: 1, Height: 1},
		{Width: 0, Height: 0},
	} {
		updated, _ := testWelcome().Update(size)
		view := updated.View()
		if !view.AltScreen || view.BackgroundColor == nil {
			t.Fatal("expected themed alternate-screen view")
		}
		if size.Width == 0 || size.Height == 0 {
			if view.Content != "" {
				t.Fatal("zero-sized terminal should have an empty view")
			}
			continue
		}
		if lipgloss.Width(view.Content) > size.Width || lipgloss.Height(view.Content) != size.Height {
			t.Fatalf("terminal %dx%d, view %dx%d", size.Width, size.Height, lipgloss.Width(view.Content), lipgloss.Height(view.Content))
		}
		if size.Width >= 80 {
			plain := ansi.Strip(view.Content)
			lines := strings.Split(plain, "\n")
			if !strings.Contains(lines[len(lines)-1], "Violet · Welcome") {
				t.Fatal("status bar is not at the bottom")
			}
			if !strings.Contains(plain, "Welcome to Violet") || !strings.Contains(plain, "Operators") || !strings.Contains(plain, "Commands") || !strings.Contains(plain, `\theme`) {
				t.Fatal("missing welcome content")
			}
			if strings.TrimSpace(lines[0]) != "" {
				t.Fatal("welcome box should have space above it")
			}
		}
	}
}

func TestWelcomeKeyBindings(t *testing.T) {
	_, cmd := testWelcome().Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+d did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+d did not return a quit message")
	}
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q'},
		{Code: 'x'},
		{Code: 'c', Mod: tea.ModCtrl},
		{Code: tea.KeyEscape, Mod: tea.ModCtrl},
	} {
		if _, cmd := testWelcome().Update(key); cmd != nil {
			t.Fatalf("%s should not quit", key.String())
		}
	}
	updated, cmd := testWelcome().Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil || !updated.(Welcome).command.Focused() {
		t.Fatal("esc should focus the command bar")
	}
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if updated.(Welcome).command.Value() != "q" {
		t.Fatal("q should be ordinary text in the command bar")
	}
	updated, cmd = updated.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil || updated.(Welcome).command.Value() != "q" {
		t.Fatal("ctrl+c should do nothing while the command bar is focused")
	}
}
