package pages

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/ui/components"
)

func testPaletteCreator() PaletteCreator {
	p := components.VioletPalette
	return NewPaletteCreator(components.NewStyles(p.Primary, p.Secondary, p.Accent, p.Background), p, []string{"Violet"})
}

func TestPaletteCreatorNavigationAndPreview(t *testing.T) {
	m := testPaletteCreator()
	updated, _ := m.Update(tea.PasteMsg{Content: "My palette"})
	m = updated.(PaletteCreator)
	if m.inputs[0].Value() != "My palette" {
		t.Fatal("name paste failed")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(PaletteCreator)
	if m.focus != 1 {
		t.Fatal("tab did not navigate")
	}
	m.inputs[1].SetValue("")
	updated, _ = m.Update(tea.PasteMsg{Content: "#123456"})
	m = updated.(PaletteCreator)
	if m.palette.Primary != (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 255}) {
		t.Fatal("valid paste did not preview")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated.(PaletteCreator)
	if m.focus != 2 {
		t.Fatal("vim j did not navigate colors")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k'})
	m = updated.(PaletteCreator)
	if m.focus != 1 {
		t.Fatal("vim k did not navigate colors")
	}
	m.inputs[1].SetValue("bad")
	preview := m.palette
	updated, _ = m.Update(tea.PasteMsg{Content: "!"})
	m = updated.(PaletteCreator)
	if m.palette != preview {
		t.Fatal("invalid input changed preview")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("invalid input saved")
	}
	m.inputs[1].SetValue("#123456")
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("valid form did not save")
	}
	saved, ok := cmd().(PaletteCreatedMsg)
	if !ok || saved.Name != "My palette" || saved.Palette.Primary != preview.Primary {
		t.Fatal("wrong created palette", saved)
	}
}

func TestPaletteCreatorEachColorPreviews(t *testing.T) {
	for index := 1; index <= 4; index++ {
		m := testPaletteCreator()
		m.inputs[m.focus].Blur()
		m.focus = index
		m.inputs[index].Focus()
		m.inputs[index].SetValue("")
		red, green, blue := byte(index*0x11), byte(index*0x22), byte(index*0x33)
		updated, _ := m.Update(tea.PasteMsg{Content: fmt.Sprintf("#%02X%02X%02X", red, green, blue)})
		m = updated.(PaletteCreator)
		want := color.NRGBA{R: red, G: green, B: blue, A: 255}
		var got color.Color
		switch index {
		case 1:
			got = m.palette.Primary
		case 2:
			got = m.palette.Secondary
		case 3:
			got = m.palette.Accent
		case 4:
			got = m.palette.Background
		}
		if got != want {
			t.Fatalf("field %d did not preview %v", index, want)
		}
	}
}

func TestPaletteCreatorCancelValidationAndKeys(t *testing.T) {
	m := testPaletteCreator()
	for _, name := range []string{"", "Violet", "  "} {
		m.inputs[0].SetValue(name)
		if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
			t.Fatalf("invalid name %q saved", name)
		}
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	m = updated.(PaletteCreator)
	if m.focus != 1 {
		t.Fatal("ctrl+j did not navigate")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if updated.(PaletteCreator).focus != 0 {
		t.Fatal("shift+tab did not navigate")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmd().(PaletteCreationCancelledMsg); !ok {
		t.Fatal("escape did not cancel")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("ctrl+c should do nothing")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+d should quit")
	}
}

func TestPaletteCreatorMouseAndSmallViews(t *testing.T) {
	m := testPaletteCreator()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	found := false
	for y, line := range lines {
		if strings.Contains(line, "Background ") {
			updated, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 30, Y: y})
			if updated.(PaletteCreator).focus != 4 {
				t.Fatal("mouse did not focus background")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("background field not rendered")
	}
	for _, size := range [][2]int{{0, 0}, {1, 1}, {4, 2}, {20, 8}, {80, 24}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		_ = updated.View()
	}
}
