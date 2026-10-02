package pages

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tristanisham/violet/ui/components"
)

type PaletteCreatedMsg struct {
	Name    string
	Palette components.Palette
}
type PaletteCreationCancelledMsg struct{}

type paletteFormKeys struct {
	next, previous, save, cancel key.Binding
}

func (k paletteFormKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.next, k.previous, k.save, k.cancel}
}
func (k paletteFormKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

var paletteLabels = []string{"Name", "Primary", "Secondary", "Accent", "Background"}

type PaletteCreator struct {
	inputs        []textinput.Model
	focus         int
	palette       components.Palette
	styles        components.Styles
	names         []string
	width, height int
	help          help.Model
	keys          paletteFormKeys
	errorText     string
	backdrop      string
}

func NewPaletteCreator(styles components.Styles, palette components.Palette, names []string) PaletteCreator {
	m := PaletteCreator{palette: palette, styles: styles, names: names, width: 80, height: 24, help: help.New()}
	m.keys = paletteFormKeys{
		next:     key.NewBinding(key.WithKeys("tab", "down", "ctrl+j"), key.WithHelp("tab/↓/j", "next")),
		previous: key.NewBinding(key.WithKeys("shift+tab", "up", "ctrl+k"), key.WithHelp("shift+tab/↑/k", "prev")),
		save:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
		cancel:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
	data, _ := json.Marshal(palette)
	var colors map[string]string
	_ = json.Unmarshal(data, &colors)
	for i, label := range paletteLabels {
		input := textinput.New()
		input.Prompt = ""
		input.CharLimit = 64
		if i == 0 {
			input.Placeholder = "Unique palette name"
		} else {
			input.CharLimit = 9
			input.SetValue(colors[strings.ToLower(label)])
		}
		m.inputs = append(m.inputs, input)
	}
	m.inputs[0].Focus()
	return m
}

func (m PaletteCreator) Init() tea.Cmd                              { return textinput.Blink }
func (m PaletteCreator) PreviewStyles() components.Styles           { return m.styles }
func (m PaletteCreator) WithBackdrop(content string) PaletteCreator { m.backdrop = content; return m }

func (m *PaletteCreator) move(delta int) tea.Cmd {
	m.inputs[m.focus].Blur()
	m.focus = (m.focus + delta + len(m.inputs)) % len(m.inputs)
	return m.inputs[m.focus].Focus()
}

func (m PaletteCreator) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(0, msg.Width), max(0, msg.Height)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && msg.X >= 0 && msg.X < m.width && msg.Y >= 0 && msg.Y < m.height {
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if msg.Y < len(lines) {
				line := lines[msg.Y]
				left, right := strings.Index(line, "│"), strings.LastIndex(line, "│")
				if left >= 0 && left < right && msg.X > lipgloss.Width(line[:left]) && msg.X < lipgloss.Width(line[:right]) {
					text := strings.TrimLeft(line[left+len("│"):right], " >")
					for index, label := range paletteLabels {
						if strings.HasPrefix(text, label+" ") {
							return m, m.move(index - m.focus)
						}
					}
				}
			}
		}
		return m, nil
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelDown:
			return m, m.move(1)
		case tea.MouseWheelUp:
			return m, m.move(-1)
		}
		return m, nil
	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+d":
			return m, tea.Quit
		case msg.String() == "ctrl+c":
			return m, nil
		case key.Matches(msg, m.keys.cancel):
			return m, func() tea.Msg { return PaletteCreationCancelledMsg{} }
		case key.Matches(msg, m.keys.next), m.focus > 0 && msg.String() == "j":
			return m, m.move(1)
		case key.Matches(msg, m.keys.previous), m.focus > 0 && msg.String() == "k":
			return m, m.move(-1)
		case key.Matches(msg, m.keys.save):
			palette, err := m.formPalette()
			name := strings.TrimSpace(m.inputs[0].Value())
			if err != nil {
				m.errorText = err.Error()
				return m, nil
			}
			if name == "" {
				m.errorText = "Enter a palette name"
				return m, nil
			}
			for _, existing := range m.names {
				if existing == name {
					m.errorText = "That palette name already exists"
					return m, nil
				}
			}
			return m, func() tea.Msg { return PaletteCreatedMsg{Name: name, Palette: palette} }
		}
	}
	// Focus is initialized here so a value receiver does not lose the focused input.
	if !m.inputs[m.focus].Focused() {
		m.inputs[m.focus].Focus()
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	if m.focus > 0 {
		value := strings.TrimSpace(m.inputs[m.focus].Value())
		if len(value) == 7 {
			data, _ := json.Marshal(map[string]string{strings.ToLower(paletteLabels[m.focus]): value})
			palette := m.palette
			if err := json.Unmarshal(data, &palette); err == nil {
				m.palette = palette
				m.styles = components.NewStyles(palette.Primary, palette.Secondary, palette.Accent, palette.Background)
				m.errorText = ""
			}
		}
	}
	return m, cmd
}

func (m PaletteCreator) formPalette() (components.Palette, error) {
	colors := make(map[string]string, 4)
	for i := 1; i < len(m.inputs); i++ {
		value := strings.TrimSpace(m.inputs[i].Value())
		if len(value) != 7 {
			return m.palette, fmt.Errorf("%s must be #RRGGBB", paletteLabels[i])
		}
		colors[strings.ToLower(paletteLabels[i])] = value
	}
	data, _ := json.Marshal(colors)
	var palette components.Palette
	if err := json.Unmarshal(data, &palette); err != nil {
		return m.palette, err
	}
	return palette, nil
}

func (m PaletteCreator) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	width := max(1, min(70, m.width-4))
	inner := max(1, width-m.styles.Box.GetHorizontalFrameSize())
	var rows []string
	rows = append(rows, m.styles.Title.Render("Create a palette"), "")
	for i, input := range m.inputs {
		input.SetWidth(max(1, inner-13))
		state := textinput.StyleState{Text: m.styles.Text, Prompt: m.styles.Text, Placeholder: m.styles.Text.Faint(true)}
		input.SetStyles(textinput.Styles{Focused: state, Blurred: state, Cursor: textinput.CursorStyle{Color: m.styles.Title.GetForeground(), Shape: tea.CursorBlock}})
		marker := "  "
		if i == m.focus {
			marker = "> "
		}
		rows = append(rows, m.styles.Text.Render(fmt.Sprintf("%s%-11s", marker, paletteLabels[i]))+input.View())
	}
	rows = append(rows, "", m.styles.Text.Render("j/k: color fields · ctrl+j/k: any field"))
	if m.errorText != "" {
		rows = append(rows, m.styles.Title.Render(m.errorText))
	}
	m.help.SetWidth(inner)
	m.help.Styles.ShortKey = m.styles.Title
	m.help.Styles.ShortDesc = m.styles.Text
	m.help.Styles.ShortSeparator = m.styles.Text
	rows = append(rows, "", m.help.View(m.keys))
	modal := m.styles.Box.Width(inner).MaxWidth(m.width).MaxHeight(m.height).Render(strings.Join(rows, "\n"))
	backdrop := m.backdrop
	if backdrop == "" {
		backdrop = m.styles.Canvas.Width(m.width).Height(m.height).Render("")
	}
	x, y := max(0, (m.width-lipgloss.Width(modal))/2), max(0, (m.height-lipgloss.Height(modal))/2)
	content := lipgloss.NewCompositor(lipgloss.NewLayer(backdrop), lipgloss.NewLayer(modal).X(x).Y(y).Z(1)).Render()
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.BackgroundColor = m.styles.Background
	view.WindowTitle = "Violet · Create palette"
	return view
}
