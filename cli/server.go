package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/server"

	opts "github.com/urfave/cli/v3"
)

// func ServerHandler(ctx context.Context, c *opts.Command) error {
// 	return nil
// }

func ServerStartHandler(ctx context.Context, c *opts.Command) error {
	state, ok := c.Root().Metadata["state"].(*meta.State)
	if !ok || state == nil {
		return meta.ErrStaleState
	}

	s := server.NewEngine()
	// --listen may be undefined on older command trees; String returns "" then.
	if listen := c.String("listen"); listen != "" {
		s.Addr = listen
	}
	c.Root().Metadata["server"] = s

	// Bind first so an invalid or refused --listen fails before the engine starts.
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	if err := s.Start(state); err != nil {
		ln.Close()
		return err
	}
	defer s.Stop()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.Serve(ctx, ln, state)
}

func ServerStatusHandler(ctx context.Context, c *opts.Command) error {
	state, ok := c.Root().Metadata["state"].(*meta.State)
	if !ok || state == nil {
		return meta.ErrStaleState
	}

	return nil
}

func ServerStopHandler(ctx context.Context, c *opts.Command) error {
	state, ok := c.Root().Metadata["state"].(*meta.State)
	if !ok || state == nil {
		return meta.ErrStaleState
	}

	s, ok := c.Root().Metadata["server"].(*server.Engine)
	if !ok || s == nil {
		return fmt.Errorf("no server running in this process")
	}

	s.Stop()
	delete(c.Root().Metadata, "server")
	return nil
}
