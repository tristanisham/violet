package pages

import (
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/ui/components"
)

type ThemeChoice struct {
	Name   string
	Styles components.Styles
}

func (t ThemeChoice) Title() string       { return t.Name }
func (t ThemeChoice) Description() string { return "" }
func (t ThemeChoice) FilterValue() string { return t.Name }

type ThemeSelectedMsg struct{ Name string }
type ThemeCancelledMsg struct{}

type ThemePicker struct {
	choices []ThemeChoice
	list    list.Model
	width   int
	height  int
}

func NewThemePicker(choices []ThemeChoice, selectedName string) ThemePicker {
	items := make([]list.Item, len(choices))
	selected := 0
	for i, choice := range choices {
		items[i] = choice
		if choice.Name == selectedName {
			selected = i
		}
	}
	m := ThemePicker{choices: choices, width: 80, height: 24}
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	m.list = list.New(items, delegate, 48, len(choices))
	m.list.SetFilteringEnabled(false)
	m.list.SetShowTitle(false)
	m.list.SetShowStatusBar(false)
	m.list.SetShowHelp(false)
	m.list.SetShowPagination(false)
	m.list.DisableQuitKeybindings()
	m.list.Select(selected)
	return m
}

func (m ThemePicker) Init() tea.Cmd { return nil }

func (m ThemePicker) SelectedName() string {
	if len(m.choices) == 0 {
		return ""
	}
	return m.choices[m.list.Index()].Name
}

func (m ThemePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(0, msg.Width), max(0, msg.Height)
		m.list.SetSize(max(1, min(46, m.width-10)), max(1, min(len(m.choices), m.height-7)))
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if index, ok := m.themeAt(msg.X, msg.Y); ok {
				m.list.Select(index)
			}
		}
		return m, nil
	case tea.MouseWheelMsg:
		key := tea.KeyPressMsg{}
		switch msg.Button {
		case tea.MouseWheelUp:
			key.Code = tea.KeyUp
		case tea.MouseWheelDown:
			key.Code = tea.KeyDown
		default:
			return m, nil
		}
		return m.Update(key)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+d":
			return m, tea.Quit
		case "ctrl+c", "q":
			return m, nil
		case "esc":
			return m, func() tea.Msg { return ThemeCancelledMsg{} }
		case "enter":
			name := m.SelectedName()
			return m, func() tea.Msg { return ThemeSelectedMsg{Name: name} }
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ThemePicker) themeAt(x, y int) (int, bool) {
	if x < 0 || x >= m.width || y < 0 || y >= m.height-1 {
		return 0, false
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if y >= len(lines) {
		return 0, false
	}
	line := lines[y]
	left, right := strings.Index(line, "│"), strings.LastIndex(line, "│")
	if left < 0 || left == right || x <= lipgloss.Width(line[:left]) || x >= lipgloss.Width(line[:right]) {
		return 0, false
	}
	name := strings.Trim(line, " │")
	for index, choice := range m.choices {
		if name == choice.Name {
			return index, true
		}
	}
	return 0, false
}

func (m ThemePicker) View() tea.View {
	if len(m.choices) == 0 || m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	styles := m.choices[m.list.Index()].Styles
	status := components.StatusBar(m.width, "Theme · "+m.SelectedName(), "j/k or ↑/↓ · enter: select · esc: cancel ", styles)
	content := status
	if m.height > 1 {
		bodyHeight := m.height - 1
		boxWidth := max(1, min(52, m.width-4))
		textWidth := max(1, boxWidth-styles.Box.GetHorizontalFrameSize())
		delegate := list.NewDefaultDelegate()
		delegate.ShowDescription = false
		delegate.SetHeight(1)
		delegate.SetSpacing(0)
		delegate.Styles.NormalTitle = styles.Text.PaddingLeft(2)
		delegate.Styles.SelectedTitle = styles.Title.PaddingLeft(1).Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(styles.Title.GetForeground()).BorderBackground(styles.Background)
		m.list.SetDelegate(delegate)
		m.list.SetSize(textWidth, max(1, min(len(m.choices), bodyHeight-6)))
		title := styles.Title.Width(textWidth).Align(lipgloss.Center).Render("Choose a theme")
		box := styles.Box.Width(textWidth).MaxWidth(m.width).MaxHeight(bodyHeight).Render(title + "\n\n" + m.list.View())
		body := styles.Canvas.Width(m.width).Height(bodyHeight).MaxWidth(m.width).MaxHeight(bodyHeight).Align(lipgloss.Center, lipgloss.Center).Render(box)
		content = body + "\n" + status
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = styles.Background
	view.WindowTitle = "Violet · Themes"
	return view
}
