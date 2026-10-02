package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/tristanisham/violet/protocol"
)

// submitTimeout bounds only the Submit call (admission). Accepted work belongs
// to the server and is not tied to this context.
const submitTimeout = 15 * time.Second

// errOffline is shown when the TUI runs without any server connection.
var errOffline = errors.New("offline: no Violet server is connected; run inside a project with violet.toml or pass --server")

type subscribedMsg struct{ events <-chan protocol.Event }

type subscribeFailedMsg struct{ err error }

type eventMsg struct{ event protocol.Event }

type disconnectedMsg struct{}

type submitFailedMsg struct {
	request protocol.Request
	err     error
}

func subscribeCmd(ctx context.Context, client protocol.Client) tea.Cmd {
	return func() tea.Msg {
		events, err := client.Subscribe(ctx)
		if err != nil {
			return subscribeFailedMsg{err: err}
		}
		return subscribedMsg{events: events}
	}
}

func listenCmd(events <-chan protocol.Event) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return disconnectedMsg{}
		}
		return eventMsg{event: event}
	}
}

func submitCmd(ctx context.Context, client protocol.Client, req protocol.Request) tea.Cmd {
	return func() tea.Msg {
		submitCtx, cancel := context.WithTimeout(ctx, submitTimeout)
		defer cancel()
		if err := client.Submit(submitCtx, req); err != nil {
			return submitFailedMsg{request: req, err: err}
		}
		return nil
	}
}

// sanitize removes control characters (including ESC) from server-supplied
// text so a hostile remote server cannot inject terminal escape sequences.
func sanitize(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

func eventError(event protocol.Event) error {
	if event.Error != "" {
		return errors.New(sanitize(event.Error))
	}
	return fmt.Errorf("%s request failed", event.Subject)
}

func decodeModels(event protocol.Event) ([]protocol.CatalogModel, error) {
	var models []protocol.CatalogModel
	if err := json.Unmarshal(event.Data, &models); err != nil {
		return nil, fmt.Errorf("decode model catalog: %w", err)
	}
	for i := range models {
		models[i].ID = sanitize(models[i].ID)
		models[i].Name = sanitize(models[i].Name)
		models[i].Description = sanitize(models[i].Description)
	}
	return models, nil
}

func subjectLabel(subject protocol.Subject) string {
	switch subject {
	case protocol.SubjectThemeSelect:
		return "theme"
	case protocol.SubjectPaletteCreate:
		return "palette"
	case protocol.SubjectGraphicsGet:
		return "settings"
	case protocol.SubjectModels:
		return "models"
	default:
		return string(subject)
	}
}
