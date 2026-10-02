package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/ui/components"
	"github.com/tristanisham/violet/ui/pages"
)

func TestThemeNavigation(t *testing.T) {
	model := NewModel()
	original := model.View().BackgroundColor
	updated, _ := model.Update(components.CommandSubmittedMsg{Text: `\theme`})
	model = updated.(Model)
	if _, ok := model.page.(pages.ThemePicker); !ok {
		t.Fatal("\\theme did not open picker")
	}
	for _, theme := range model.themes {
		if !strings.Contains(ansi.Strip(model.View().Content), theme.Name) {
			t.Fatalf("theme %s is missing", theme.Name)
		}
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'j'})
	model = updated.(Model)
	if reflect.DeepEqual(model.View().BackgroundColor, original) {
		t.Fatal("theme preview did not change background")
	}
	picker := model.page.(pages.ThemePicker)
	chosen := picker.SelectedName()
	preview := model.View().BackgroundColor
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not select a theme")
	}
	updated, _ = updated.Update(cmd())
	model = updated.(Model)
	if _, ok := model.page.(pages.Welcome); !ok || model.selected != chosen {
		t.Fatal("selection did not return to the original page")
	}
	if !reflect.DeepEqual(model.View().BackgroundColor, preview) {
		t.Fatal("selected theme was not applied to the previous page")
	}
	updated, _ = model.Update(components.CommandSubmittedMsg{Text: `\theme`})
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	updated, cmd = updated.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	updated, _ = updated.Update(cmd())
	if !reflect.DeepEqual(updated.View().BackgroundColor, preview) {
		t.Fatal("cancel did not restore selected theme")
	}
}

func TestOnlyBackslashOpensThemePicker(t *testing.T) {
	for _, text := range []string{"/theme", "^theme", "theme"} {
		updated, _ := NewModel().Update(components.CommandSubmittedMsg{Text: text})
		if _, ok := updated.(Model).page.(pages.Welcome); !ok {
			t.Fatalf("%q should be ordinary text", text)
		}
	}
}

func TestThemePickerKeys(t *testing.T) {
	model := NewModel()
	picker := pages.NewThemePicker(model.themes, "Violet")
	updated, _ := picker.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'j'})
	if updated.(pages.ThemePicker).SelectedName() != "Forest" {
		t.Fatal("arrows and j should advance selection")
	}
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'k'})
	updated, _ = updated.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if updated.(pages.ThemePicker).SelectedName() != "Violet" {
		t.Fatal("arrows and k should move up")
	}
	for _, key := range []tea.KeyPressMsg{{Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}} {
		if _, cmd := updated.Update(key); cmd != nil {
			t.Fatal("q and ctrl+c should not quit")
		}
	}
	_, cmd := updated.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+d should quit")
	}
}
