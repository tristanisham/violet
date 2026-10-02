package pages

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/ui/components"
)

type ModelsRequestedMsg struct{}

type ModelsLoadedMsg struct {
	RequestID string
	Models    []protocol.CatalogModel
	Err       error
}

type ProvidersClosedMsg struct{}

type providerItem struct {
	name    string
	enabled bool
}

func (p providerItem) FilterValue() string { return p.name }

type providerDelegate struct{ styles components.Styles }

func (d providerDelegate) Height() int                         { return 1 }
func (d providerDelegate) Spacing() int                        { return 0 }
func (d providerDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d providerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	provider := item.(providerItem)
	style := d.styles.Text
	marker := "  "
	if !provider.enabled {
		style = style.Foreground(lipgloss.Color("#808080")).Faint(true)
	} else if index == m.Index() {
		style = d.styles.Title
		marker = "> "
	}
	fmt.Fprint(w, style.Render(ansi.Truncate(marker+provider.name, m.Width(), "…")))
}

type catalogItem struct{ model protocol.CatalogModel }

func (m catalogItem) Title() string       { return m.model.Name }
func (m catalogItem) Description() string { return m.model.Description }
func (m catalogItem) FilterValue() string { return m.model.Name }

type providerKeys struct{ focus, move, refresh, close key.Binding }

func (k providerKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.focus, k.move, k.refresh, k.close}
}
func (k providerKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

type Providers struct {
	providers     list.Model
	models        list.Model
	styles        components.Styles
	width, height int
	modelFocus    bool
	help          help.Model
	keys          providerKeys
	requestID     string
	loading       bool
	errorText     string
}

func NewProviders(styles components.Styles) Providers {
	names := []string{"Cloudflare", "Vercel", "Google", "OpenAI", "Anthropic", "OpenRouter", "OpenCode"}
	sort.Strings(names)
	items := make([]list.Item, len(names))
	selected := 0
	for i, name := range names {
		items[i] = providerItem{name: name, enabled: name == "Cloudflare"}
		if name == "Cloudflare" {
			selected = i
		}
	}
	m := Providers{styles: styles, width: 80, height: 24, modelFocus: true, help: help.New(), loading: true}
	m.keys = providerKeys{
		focus:   key.NewBinding(key.WithKeys("tab", "shift+tab", "h", "l", "left", "right"), key.WithHelp("tab/h/l", "pane")),
		move:    key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓/j/k", "scroll")),
		refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
	m.providers = list.New(items, providerDelegate{styles: styles}, 20, 20)
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	m.models = list.New(nil, delegate, 50, 20)
	for _, model := range []*list.Model{&m.providers, &m.models} {
		model.SetFilteringEnabled(false)
		model.SetShowTitle(false)
		model.SetShowStatusBar(false)
		model.SetShowHelp(false)
		model.SetShowPagination(false)
		model.DisableQuitKeybindings()
	}
	m.providers.Select(selected)
	m.layout()
	return m
}

func (m Providers) Init() tea.Cmd {
	return func() tea.Msg { return ModelsRequestedMsg{} }
}

// SetRequestID associates the root client's next catalog response with this page.
// The root must use a new ID for every request, including after reopening the page.
func (m Providers) SetRequestID(id string) Providers {
	m.requestID = id
	m.loading, m.errorText = true, ""
	return m
}

func (m Providers) SetStyles(styles components.Styles) tea.Model {
	m.styles = styles
	m.layout()
	return m
}

func (m Providers) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(0, msg.Width), max(0, msg.Height)
		m.layout()
		return m, nil
	case ModelsLoadedMsg:
		if !m.loading || m.requestID == "" || msg.RequestID != m.requestID {
			return m, nil
		}
		m.loading, m.requestID, m.errorText = false, "", ""
		if msg.Err != nil {
			m.errorText = msg.Err.Error()
			m.models.SetItems(nil)
			return m, nil
		}
		items := make([]list.Item, len(msg.Models))
		for i, model := range msg.Models {
			items[i] = catalogItem{model: model}
		}
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].FilterValue() < items[j].FilterValue()
		})
		m.models.SetItems(items)
		m.models.ResetSelected()
		return m, nil
	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+d":
			m.requestID = ""
			return m, tea.Quit
		case msg.String() == "ctrl+c" || msg.String() == "q":
			return m, nil
		case key.Matches(msg, m.keys.close):
			m.requestID = ""
			return m, func() tea.Msg { return ProvidersClosedMsg{} }
		case key.Matches(msg, m.keys.refresh):
			m.requestID = ""
			m.loading, m.errorText = true, ""
			return m, m.Init()
		case key.Matches(msg, m.keys.focus):
			switch msg.String() {
			case "h", "left":
				m.modelFocus = false
			case "l", "right":
				m.modelFocus = true
			default:
				m.modelFocus = !m.modelFocus
			}
			return m, nil
		}
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if index := m.rowAt(m.providers, 0, m.sidebarWidth(), msg.X, msg.Y); index >= 0 {
			if m.providers.Items()[index].(providerItem).enabled {
				m.providers.Select(index)
				m.modelFocus = false
			}
		} else if index := m.rowAt(m.models, m.sidebarWidth(), m.width-m.sidebarWidth(), msg.X, msg.Y); index >= 0 && !m.loading && m.errorText == "" {
			m.models.Select(index)
			m.modelFocus = true
		}
		return m, nil
	case tea.MouseWheelMsg:
		if msg.X < m.sidebarWidth() || msg.X >= m.width || msg.Y < 0 || msg.Y >= m.height-1 {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.updateModels(tea.KeyPressMsg{Code: tea.KeyUp})
		case tea.MouseWheelDown:
			return m.updateModels(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		return m, nil
	}
	if m.modelFocus {
		return m.updateModels(msg)
	}
	return m, nil
}

func (m Providers) updateModels(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.loading || m.errorText != "" {
		return m, nil
	}
	var cmd tea.Cmd
	m.models, cmd = m.models.Update(msg)
	return m, cmd
}

func (m Providers) sidebarWidth() int { return min(24, m.width/3) }

func (m *Providers) layout() {
	border := m.styles.Box.Padding(0, 1)
	listHeight := max(1, m.height-1-border.GetVerticalFrameSize()-2)
	m.providers.SetSize(max(1, m.sidebarWidth()-border.GetHorizontalFrameSize()), listHeight)
	m.providers.SetDelegate(providerDelegate{styles: m.styles})
	m.models.SetSize(max(1, m.width-m.sidebarWidth()-border.GetHorizontalFrameSize()), listHeight)
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	delegate.Styles.NormalTitle = m.styles.Text.PaddingLeft(2)
	delegate.Styles.SelectedTitle = m.styles.Title.PaddingLeft(1).Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(m.styles.Title.GetForeground())
	m.models.SetDelegate(delegate)
}

func (m Providers) rowAt(model list.Model, left, width, x, y int) int {
	border := m.styles.Box.Padding(0, 1)
	rowTop := border.GetBorderTopSize() + border.GetPaddingTop() + 2
	rowLeft := left + border.GetBorderLeftSize() + border.GetPaddingLeft()
	rowRight := left + width - border.GetBorderRightSize() - border.GetPaddingRight()
	row := y - rowTop
	if x < rowLeft || x >= rowRight || row < 0 || row >= model.Height() || y >= m.height-1-border.GetBorderBottomSize() {
		return -1
	}
	index := model.Paginator.Page*model.Paginator.PerPage + row
	if index >= len(model.Items()) {
		return -1
	}
	return index
}

func (m Providers) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	m.help.SetWidth(m.width)
	m.help.Styles.ShortKey = m.styles.Title
	m.help.Styles.ShortDesc = m.styles.Text
	m.help.Styles.ShortSeparator = m.styles.Text
	footer := m.styles.Text.Width(m.width).MaxWidth(m.width).MaxHeight(1).Render(m.help.View(m.keys))
	content := footer
	if m.height > 1 {
		height := m.height - 1
		leftWidth := m.sidebarWidth()
		rightWidth := max(0, m.width-leftWidth)
		border := m.styles.Box.Padding(0, 1)
		leftInner := max(1, leftWidth-border.GetHorizontalFrameSize())

		left := border.Width(leftInner).Height(max(1, height-border.GetVerticalFrameSize())).MaxWidth(leftWidth).MaxHeight(height).Render(m.styles.Title.Render("Providers") + "\n\n" + m.providers.View())
		rightInner := max(1, rightWidth-border.GetHorizontalFrameSize())

		models := m.models.View()
		switch {
		case m.loading:
			models = "Loading Cloudflare models…"
		case m.errorText != "":
			models = ansi.Wrap(m.errorText+"\n\nPress r to retry. No inference request was sent.", rightInner, "")
		case len(m.models.Items()) == 0:
			models = "No Text Generation models found."
		}
		right := border.Width(rightInner).Height(max(1, height-border.GetVerticalFrameSize())).MaxWidth(rightWidth).MaxHeight(height).Render(m.styles.Title.Render("Cloudflare · Text Generation") + "\n\n" + m.styles.Text.Render(models))
		content = lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n" + footer
	}
	content = m.styles.Canvas.MaxWidth(m.width).MaxHeight(m.height).Render(strings.TrimRight(content, "\n"))
	lines := strings.Split(content, "\n")
	lines = lines[:min(len(lines), m.height)]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "")
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = m.styles.Background
	view.WindowTitle = "Violet · Providers"
	return view
}
