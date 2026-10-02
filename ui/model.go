package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/ui/components"
	"github.com/tristanisham/violet/ui/pages"
)

var builtinPalettes = []string{"Violet", "Misty", "Forest", "Slate", "Pumpkin", "Autumn"}

type styler interface {
	SetStyles(components.Styles) tea.Model
}

type statusSetter interface {
	SetStatus(string) tea.Model
}

// Model is the root navigation model. Presentation state (focus, previews,
// sizes) stays local; application actions go through an optional
// protocol.Client so the same TUI works in-process or against a remote server.
type Model struct {
	page     tea.Model
	previous tea.Model
	themes   []pages.ThemeChoice

	selected string
	width    int
	height   int
	graphics meta.GraphicSettings

	client       protocol.Client
	ctx          context.Context
	events       <-chan protocol.Event
	disconnected bool
	// graphicsRequest is the latest graphics.* request; only its result is applied.
	graphicsRequest string
	// modelsRequest is the outstanding models.list request for the providers page.
	modelsRequest string
	modelsPending bool
	status        string
}

// NewModel builds an offline model; graphics settings are kept in memory only.
func NewModel(graphics ...meta.GraphicSettings) Model {
	settings := *meta.NewGraphicSettings()
	if len(graphics) > 0 {
		settings = graphics[0]
	}
	m := Model{width: 80, height: 24, ctx: context.Background()}
	m.graphics = settings
	m.selected = settings.Palette
	m.themes = buildThemes(settings)
	m.page = pages.NewWelcome(m.styles())
	m.setStatus("offline")
	return m
}

// WithClient connects the model to a server through client. The subscription
// is established by Init; nothing is submitted before it exists.
func (m Model) WithClient(client protocol.Client) Model {
	m.client = client
	if client != nil {
		m.setStatus("connecting…")
	} else {
		m.setStatus("offline")
	}
	return m
}

func (m Model) withContext(ctx context.Context) Model {
	m.ctx = ctx
	return m
}

func buildThemes(settings meta.GraphicSettings) []pages.ThemeChoice {
	names := append([]string(nil), builtinPalettes...)
	var custom []string
	for name := range settings.Palettes {
		if !isBuiltin(name) {
			custom = append(custom, name)
		}
	}
	sort.Strings(custom)
	names = append(names, custom...)
	var themes []pages.ThemeChoice
	for _, name := range names {
		if palette, ok := settings.Palettes[name]; ok {
			themes = append(themes, pages.ThemeChoice{Name: name, Styles: paletteStyles(palette)})
		}
	}
	return themes
}

func isBuiltin(name string) bool {
	for _, builtin := range builtinPalettes {
		if name == builtin {
			return true
		}
	}
	return false
}

func paletteStyles(palette Palette) components.Styles {
	return components.NewStyles(palette.Primary, palette.Secondary, palette.Accent, palette.Background)
}

func (m Model) styles() components.Styles {
	return paletteStyles(m.graphics.Palettes[m.selected])
}

func (m Model) Init() tea.Cmd {
	cmd := m.page.Init()
	if m.client == nil {
		return cmd
	}
	return tea.Batch(cmd, subscribeCmd(m.ctx, m.client))
}

func (m *Model) setStatus(status string) {
	m.status = status
	if page, ok := m.page.(statusSetter); ok {
		m.page = page.SetStatus(status)
	}
	if page, ok := m.previous.(statusSetter); ok {
		m.previous = page.SetStatus(status)
	}
}

func (m *Model) submit(subject protocol.Subject, recipient string, payload any) (protocol.Request, tea.Cmd) {
	req, err := protocol.NewRequest(subject, recipient, payload)
	if err != nil {
		m.setStatus(fmt.Sprintf("%s: %v", subjectLabel(subject), err))
		return protocol.Request{}, nil
	}
	return req, submitCmd(m.ctx, m.client, req)
}

func (m *Model) connected() bool {
	return m.client != nil && m.events != nil && !m.disconnected
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg, tea.MouseMotionMsg:
		if !m.graphics.MouseEnabled {
			return m, nil
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case subscribedMsg:
		m.events = msg.events
		m.disconnected = false
		m.setStatus("")
		req, cmd := m.submit(protocol.SubjectGraphicsGet, "", struct{}{})
		m.graphicsRequest = req.ID
		cmds := []tea.Cmd{listenCmd(m.events), cmd}
		if m.modelsPending {
			cmds = append(cmds, m.requestModels())
		}
		return m, tea.Batch(cmds...)
	case subscribeFailedMsg:
		m.disconnected = true
		m.setStatus("offline: " + msg.err.Error())
		m.failPendingModels(msg.err)
		return m, nil
	case eventMsg:
		cmd := m.handleEvent(msg.event)
		return m, tea.Batch(listenCmd(m.events), cmd)
	case disconnectedMsg:
		m.events = nil
		m.disconnected = true
		m.setStatus("disconnected from server")
		m.failPendingModels(protocol.ErrDisconnected)
		return m, nil
	case submitFailedMsg:
		if msg.request.Subject == protocol.SubjectModels {
			m.deliverModels(pages.ModelsLoadedMsg{RequestID: msg.request.ID, Err: msg.err})
			return m, nil
		}
		m.setStatus(fmt.Sprintf("%s: %v", subjectLabel(msg.request.Subject), msg.err))
		// An optimistic theme or palette change never reached the server:
		// resynchronize so the screen matches what is actually persisted.
		if msg.request.Subject != protocol.SubjectGraphicsGet && m.connected() {
			req, cmd := m.submit(protocol.SubjectGraphicsGet, "", struct{}{})
			m.graphicsRequest = req.ID
			return m, cmd
		}
		return m, nil
	case components.CommandSubmittedMsg:
		switch strings.TrimSpace(msg.Text) {
		case `\theme`:
			m.previous = m.page
			picker := pages.NewThemePicker(m.themes, m.selected)
			m.page, _ = picker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			return m, nil
		case `\create-palette`:
			m.previous = m.page
			var names []string
			for name := range m.graphics.Palettes {
				names = append(names, name)
			}
			palette := m.graphics.Palettes[m.selected]
			creator := pages.NewPaletteCreator(paletteStyles(palette), palette, names)
			m.page, _ = creator.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			return m, m.page.Init()
		case `\providers`:
			m.previous = m.page
			providers := pages.NewProviders(m.styles())
			m.page, _ = providers.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			// The page's own Init only asks for a catalog; request it directly.
			return m, m.requestModels()
		}
	case pages.ModelsRequestedMsg:
		return m, m.requestModels()
	case pages.ProvidersClosedMsg:
		m.modelsRequest, m.modelsPending = "", false
		return m.restorePage(), nil
	case pages.PaletteCreatedMsg:
		settings := m.graphics
		settings.Palettes = make(map[string]Palette, len(m.graphics.Palettes)+1)
		for name, palette := range m.graphics.Palettes {
			settings.Palettes[name] = palette
		}
		settings.Palettes[msg.Name] = msg.Palette
		settings.Palette = msg.Name
		m.graphics = settings
		m.selected = msg.Name
		m.themes = buildThemes(settings)
		if page, ok := m.previous.(styler); ok {
			m.previous = page.SetStyles(paletteStyles(msg.Palette))
		}
		var cmd tea.Cmd
		if m.client != nil {
			var req protocol.Request
			req, cmd = m.submit(protocol.SubjectPaletteCreate, "", struct {
				Name    string             `json:"name"`
				Palette components.Palette `json:"palette"`
			}{msg.Name, msg.Palette})
			m.graphicsRequest = req.ID
		}
		return m.restorePage(), cmd
	case pages.PaletteCreationCancelledMsg:
		return m.restorePage(), nil
	case pages.ThemeSelectedMsg:
		var cmd tea.Cmd
		for _, theme := range m.themes {
			if theme.Name != msg.Name {
				continue
			}
			m.graphics.Palette = theme.Name
			m.selected = theme.Name
			if page, ok := m.previous.(styler); ok {
				m.previous = page.SetStyles(theme.Styles)
			}
			if m.client != nil {
				var req protocol.Request
				req, cmd = m.submit(protocol.SubjectThemeSelect, "", struct {
					Name string `json:"name"`
				}{theme.Name})
				m.graphicsRequest = req.ID
			}
			break
		}
		return m.restorePage(), cmd
	case pages.ThemeCancelledMsg:
		return m.restorePage(), nil
	}
	var cmd tea.Cmd
	m.page, cmd = m.page.Update(msg)
	return m, cmd
}

// requestModels asks the server for the Cloudflare catalog on behalf of the
// providers page, giving every request a fresh ID so stale replies are ignored.
func (m *Model) requestModels() tea.Cmd {
	page, ok := m.page.(pages.Providers)
	if !ok {
		return nil
	}
	if m.client == nil {
		m.page = page.SetRequestID("offline")
		m.deliverModels(pages.ModelsLoadedMsg{RequestID: "offline", Err: errOffline})
		return nil
	}
	if m.disconnected {
		m.page = page.SetRequestID("disconnected")
		m.deliverModels(pages.ModelsLoadedMsg{RequestID: "disconnected", Err: protocol.ErrDisconnected})
		return nil
	}
	if m.events == nil {
		// Not subscribed yet: Subscribe must precede Submit, so defer the request.
		m.modelsPending = true
		return nil
	}
	m.modelsPending = false
	req, cmd := m.submit(protocol.SubjectModels, "cloudflare", struct{}{})
	if cmd == nil {
		return nil
	}
	m.modelsRequest = req.ID
	m.page = page.SetRequestID(req.ID)
	return cmd
}

func (m *Model) deliverModels(msg pages.ModelsLoadedMsg) {
	if msg.RequestID == m.modelsRequest {
		m.modelsRequest = ""
	}
	if page, ok := m.page.(pages.Providers); ok {
		m.page, _ = page.Update(msg)
	}
}

func (m *Model) failPendingModels(err error) {
	if page, ok := m.page.(pages.Providers); ok && (m.modelsRequest != "" || m.modelsPending) {
		id := m.modelsRequest
		if id == "" {
			id = "pending"
			m.page = page.SetRequestID(id)
		}
		m.modelsPending = false
		m.deliverModels(pages.ModelsLoadedMsg{RequestID: id, Err: err})
	}
}

func (m *Model) handleEvent(event protocol.Event) tea.Cmd {
	switch event.Subject {
	case protocol.SubjectModels:
		if event.RequestID == "" {
			return nil
		}
		loaded := pages.ModelsLoadedMsg{RequestID: event.RequestID}
		switch event.Kind {
		case "error":
			loaded.Err = eventError(event)
		case "result":
			loaded.Models, loaded.Err = decodeModels(event)
		default:
			return nil
		}
		m.deliverModels(loaded)
	case protocol.SubjectGraphicsGet, protocol.SubjectThemeSelect, protocol.SubjectPaletteCreate:
		switch event.Kind {
		case "error":
			m.setStatus(fmt.Sprintf("%s: %v", subjectLabel(event.Subject), eventError(event)))
			// The optimistic change was rejected: resynchronize with the server.
			if event.RequestID != "" && event.RequestID == m.graphicsRequest && event.Subject != protocol.SubjectGraphicsGet && m.connected() {
				req, cmd := m.submit(protocol.SubjectGraphicsGet, "", struct{}{})
				m.graphicsRequest = req.ID
				return cmd
			}
		case "result":
			if event.RequestID == "" || event.RequestID != m.graphicsRequest {
				return nil
			}
			var settings meta.GraphicSettings
			if err := json.Unmarshal(event.Data, &settings); err != nil {
				m.setStatus(fmt.Sprintf("settings: %v", err))
				return nil
			}
			if err := settings.Validate(); err != nil {
				m.setStatus(fmt.Sprintf("settings: %v", err))
				return nil
			}
			m.applySettings(settings)
			if event.Subject != protocol.SubjectGraphicsGet {
				m.setStatus("")
			}
		}
	}
	// Chat events are shared with every client; no page consumes them yet.
	return nil
}

// applySettings adopts the server's graphics settings. An open theme picker or
// palette creator keeps its own live preview until it is closed.
func (m *Model) applySettings(settings meta.GraphicSettings) {
	m.graphics = settings
	m.selected = settings.Palette
	m.themes = buildThemes(settings)
	styles := m.styles()
	switch m.page.(type) {
	case pages.ThemePicker, pages.PaletteCreator:
	default:
		if page, ok := m.page.(styler); ok {
			m.page = page.SetStyles(styles)
		}
	}
	if page, ok := m.previous.(styler); ok {
		m.previous = page.SetStyles(styles)
	}
}

func (m Model) restorePage() Model {
	if m.previous != nil {
		m.page, m.previous = m.previous, nil
		m.page, _ = m.page.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	}
	return m
}

func (m Model) View() tea.View {
	view := m.page.View()
	if creator, ok := m.page.(pages.PaletteCreator); ok {
		if page, ok := m.previous.(styler); ok {
			backdropPage := page.SetStyles(creator.PreviewStyles())
			backdropPage, _ = backdropPage.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			view = creator.WithBackdrop(backdropPage.View().Content).View()
		}
	}
	if !m.graphics.MouseEnabled {
		view.MouseMode = tea.MouseModeNone
	}
	return view
}
