package components

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type CommandSubmittedMsg struct {
	Text string
}

type CommandBar struct {
	input textinput.Model
	ready bool
}

func (m *CommandBar) initialize() {
	if m.ready {
		return
	}
	m.input = textinput.New()
	m.input.Prompt = ""
	m.input.Placeholder = "Esc: input · \\theme · \\create-palette · \\providers · @file: reference"
	m.ready = true
}

func (m *CommandBar) Focus() {
	m.initialize()
	m.input.Focus()
}

func (m CommandBar) Focused() bool {
	return m.ready && m.input.Focused()
}

func (m CommandBar) Value() string {
	return m.input.Value()
}

func (m CommandBar) Update(msg tea.Msg) (CommandBar, tea.Cmd) {
	m.initialize()
	if !m.input.Focused() {
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+c":
			return m, nil
		case "enter":
			text := m.input.Value()
			m.input.SetValue("")
			m.input.Blur()
			return m, func() tea.Msg { return CommandSubmittedMsg{Text: text} }
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m CommandBar) View(width int, styles Styles) string {
	if width <= 0 {
		return ""
	}
	m.initialize()
	m.input.SetWidth(max(1, width-1))
	state := textinput.StyleState{Text: styles.Text, Placeholder: styles.Text.Faint(true), Prompt: styles.Text, Suggestion: styles.Title}
	m.input.SetStyles(textinput.Styles{
		Focused: state,
		Blurred: state,
		Cursor:  textinput.CursorStyle{Color: styles.Title.GetForeground(), Shape: tea.CursorBlock},
	})
	return styles.Text.Width(width).MaxWidth(width).MaxHeight(1).Render(" " + m.input.View())
}
