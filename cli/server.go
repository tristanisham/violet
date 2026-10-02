package cli

import (
	"context"
	"fmt"

	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/server"

	opts "github.com/urfave/cli/v3"
)

// func ServerHandler(ctx context.Context, c *opts.Command) error {
// 	return nil
// }

func ServerStartHandler(ctx context.Context, c *opts.Command) error {
	settings, ok := c.Root().Metadata["settings"].(*meta.Settings)
	if !ok || settings == nil {
		return meta.ErrStaleSettings
	}

	s := server.NewServer()
	c.Root().Metadata["server"] = s

	if err := s.Start(settings); err != nil {
		return err
	}
	defer s.Stop()
	return s.StartHttp(settings)
}

func ServerStatusHandler(ctx context.Context, c *opts.Command) error {
	settings, ok := c.Root().Metadata["settings"].(*meta.Settings)
	if !ok || settings == nil {
		return meta.ErrStaleSettings
	}

	return nil
}

func ServerStopHandler(ctx context.Context, c *opts.Command) error {
	settings, ok := c.Root().Metadata["settings"].(*meta.Settings)
	if !ok || settings == nil {
		return meta.ErrStaleSettings
	}

	s, ok := c.Root().Metadata["server"].(*server.Engine)
	if !ok || s == nil {
		return fmt.Errorf("no server running in this process")
	}

	s.Stop()
	delete(c.Root().Metadata, "server")
	return nil
}
