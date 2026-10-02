package pages

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tristanisham/violet/ui/components"
)

type Welcome struct {
	width   int
	height  int
	styles  components.Styles
	command components.CommandBar
	status  string
}

func NewWelcome(styles components.Styles) Welcome {
	return Welcome{width: 80, height: 24, styles: styles}
}

func (m Welcome) Init() tea.Cmd {
	return nil
}

func (m Welcome) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(0, msg.Width)
		m.height = max(0, msg.Height)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.height >= 2 && msg.Y == m.height-2 && msg.X >= 0 && msg.X < m.width {
			m.command.Focus()
			return m, nil
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+d":
			return m, tea.Quit
		case "esc":
			m.command.Focus()
			return m, nil
		case "ctrl+c":
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.command, cmd = m.command.Update(msg)
	return m, cmd
}

func (m Welcome) SetStyles(styles components.Styles) tea.Model {
	m.styles = styles
	return m
}

// SetStatus shows a short, unobtrusive notice (for example a server error or
// disconnect) in the status bar.
func (m Welcome) SetStatus(status string) tea.Model {
	m.status = status
	return m
}

func (m Welcome) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = m.styles.Background
	view.WindowTitle = "Violet"
	return view
}

func (m Welcome) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	left := "Violet · Welcome"
	if m.status != "" {
		left += " · " + m.status
	}
	status := components.StatusBar(m.width, left, "esc: command · ctrl+d: quit ", m.styles)
	if m.height == 1 {
		return status
	}

	command := m.command.View(m.width, m.styles)
	if m.height == 2 {
		return command + "\n" + status
	}

	bodyHeight := m.height - 2
	boxWidth := max(1, min(60, m.width-4))
	textWidth := max(1, boxWidth-m.styles.Box.GetHorizontalFrameSize())
	title := m.styles.Title.Width(textWidth).Align(lipgloss.Center).Render("Welcome to Violet")
	operators := components.ReferenceList("Operators", []components.Reference{
		{Name: "\\", Description: "Command prefix"},
		{Name: "@", Description: "File reference (fuzzy / glob)"},
	}, textWidth, m.styles)
	commands := components.ReferenceList("Commands", []components.Reference{
		{Name: "\\theme", Description: "Choose a color theme"},
		{Name: "\\create-palette", Description: "Create a custom palette"},
		{Name: "\\providers", Description: "Browse provider models"},
	}, textWidth, m.styles)
	text := operators + "\n\n\n\n\n" + commands
	box := m.styles.Box.Width(boxWidth).MaxWidth(m.width).MaxHeight(bodyHeight).Render(title + "\n\n" + text)
	body := m.styles.Canvas.Width(m.width).Height(bodyHeight).MaxWidth(m.width).MaxHeight(bodyHeight).Align(lipgloss.Center, lipgloss.Center).Render(box)
	return body + "\n" + command + "\n" + status
}
