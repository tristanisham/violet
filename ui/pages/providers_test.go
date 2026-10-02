package pages

import (
	"errors"
	"image/color"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/ui/components"
)

func testProviders() Providers {
	return NewProviders(components.NewStyles(color.White, color.White, color.Black, color.Black))
}

func loaded(t *testing.T, m Providers, id string, models ...string) Providers {
	t.Helper()
	msg := ModelsLoadedMsg{RequestID: id}
	for _, name := range models {
		msg.Models = append(msg.Models, protocol.CatalogModel{ID: name, Name: name})
	}
	updated, _ := m.Update(msg)
	return updated.(Providers)
}

func TestProvidersAlphabeticalAndOnlyCloudflareEnabled(t *testing.T) {
	m := testProviders()
	var names []string
	for _, item := range m.providers.Items() {
		provider := item.(providerItem)
		names = append(names, provider.name)
		if provider.enabled != (provider.name == "Cloudflare") {
			t.Fatalf("%s enabled=%v", provider.name, provider.enabled)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("providers not alphabetical: %v", names)
	}
	if m.providers.SelectedItem().(providerItem).name != "Cloudflare" {
		t.Fatal("Cloudflare should be selected")
	}
	var sidebar []string
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		sidebar = append(sidebar, ansi.Truncate(line, m.sidebarWidth(), ""))
	}
	last := -1
	for _, name := range names {
		row := -1
		for y, line := range sidebar {
			if strings.Contains(line, name) {
				row = y
				break
			}
		}
		if row <= last {
			t.Fatalf("%s rendered out of order", name)
		}
		last = row
	}
}

func TestProvidersDisabledRowsNotClickable(t *testing.T) {
	m := testProviders()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	clicked := false
	for y, line := range lines {
		for _, name := range []string{"Anthropic", "Vercel"} {
			index := strings.Index(line, name)
			if index < 0 {
				continue
			}
			clicked = true
			updated, _ := m.Update(tea.MouseClickMsg{X: lipgloss.Width(line[:index]), Y: y, Button: tea.MouseLeft})
			if updated.(Providers).providers.SelectedItem().(providerItem).name != "Cloudflare" {
				t.Fatalf("disabled provider %s became selected", name)
			}
		}
	}
	if !clicked {
		t.Fatal("disabled provider rows not rendered")
	}
}

func TestProvidersRequestIDs(t *testing.T) {
	m := testProviders().SetRequestID("a")
	m = loaded(t, m, "b", "ignored")
	if !m.loading || len(m.models.Items()) != 0 {
		t.Fatal("mismatched reply was applied")
	}
	m = loaded(t, m, "a", "zeta", "alpha")
	if m.loading || len(m.models.Items()) != 2 || m.models.Items()[0].FilterValue() != "alpha" {
		t.Fatal("matching reply did not populate sorted models")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "alpha") || !strings.Contains(view, "zeta") {
		t.Fatal("models not rendered")
	}
	m = loaded(t, m, "a", "late")
	if len(m.models.Items()) != 2 {
		t.Fatal("duplicate reply was applied")
	}

	m = m.SetRequestID("c")
	updated, _ := m.Update(ModelsLoadedMsg{RequestID: "c", Err: errors.New("catalog unavailable")})
	m = updated.(Providers)
	if m.errorText == "" || !strings.Contains(ansi.Strip(m.View().Content), "catalog unavailable") {
		t.Fatal("error state not shown")
	}
}

func TestProvidersKeys(t *testing.T) {
	m := testProviders()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	if _, ok := cmd().(ProvidersClosedMsg); !ok {
		t.Fatal("esc should close")
	}
	updated, cmd := m.SetRequestID("x").Update(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil {
		t.Fatal("r produced no command")
	}
	if _, ok := cmd().(ModelsRequestedMsg); !ok || !updated.(Providers).loading || updated.(Providers).requestID != "" {
		t.Fatal("r should request a refresh and forget the old request")
	}
	for _, key := range []tea.KeyPressMsg{{Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}} {
		if _, cmd := m.Update(key); cmd != nil {
			t.Fatal("q and ctrl+c should be ignored")
		}
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+d should quit")
	}
}

func TestProvidersSmallSizes(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{
		{Width: 80, Height: 24}, {Width: 40, Height: 10}, {Width: 20, Height: 5},
		{Width: 8, Height: 3}, {Width: 1, Height: 1}, {Width: 0, Height: 0},
	} {
		m := loaded(t, testProviders().SetRequestID("a"), "a", "a-very-long-model-name-that-must-be-truncated")
		updated, _ := m.Update(size)
		view := updated.View()
		if size.Width == 0 || size.Height == 0 {
			if view.Content != "" {
				t.Fatal("zero-sized terminal should render nothing")
			}
			continue
		}
		if lipgloss.Width(view.Content) > size.Width || lipgloss.Height(view.Content) > size.Height {
			t.Fatalf("terminal %dx%d, view %dx%d", size.Width, size.Height, lipgloss.Width(view.Content), lipgloss.Height(view.Content))
		}
	}
}
