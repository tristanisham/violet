package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/tristanisham/violet/protocol"
)

// Start runs the TUI. With a nil client it runs offline: themes preview and
// select in memory only and nothing is persisted. Otherwise every application
// action goes through client, which may be in-process or remote. The caller
// keeps ownership of client and closes it.
func Start(client protocol.Client) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := NewModel().withContext(ctx).WithClient(client)
	_, err := tea.NewProgram(model).Run()
	return err
}
