package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/log/v2"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tristanisham/violet/cli"
	"github.com/tristanisham/violet/client"
	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/server"
	"github.com/tristanisham/violet/ui"
	opts "github.com/urfave/cli/v3"
)

func init() {
	if _, exists := os.LookupEnv("VIOLET_DEBUG"); exists {
		log.SetLevel(log.DebugLevel)
	}
}

var App = &opts.Command{
	Name:                  "violet",
	Description:           "A fun harness for local AI",
	Version:               fmt.Sprintf("v%s", meta.VERSION),
	Copyright:             fmt.Sprintf("Copyright © %d Tristan Isham", time.Now().Year()),
	Suggest:               true,
	EnableShellCompletion: true,
	// Route errors through main rather than letting the CLI exit during Run.
	ExitErrHandler: func(ctx context.Context, cmd *opts.Command, err error) {},
	Flags: []opts.Flag{
		&opts.StringFlag{
			Name:    "config",
			Aliases: []string{"c"},
			Usage:   "Path to the configuration file",
		},
		&opts.StringFlag{
			Name:    "server",
			Usage:   "URL of a remote Violet server for the TUI (token from VIOLET_SERVER_TOKEN)",
			Sources: opts.EnvVars("VIOLET_SERVER"),
		},
	},
	Before: func(ctx context.Context, c *opts.Command) (context.Context, error) {
		// With no arguments the TUI loads its own configuration (or none, for
		// --server or offline use); "init" creates the configuration.
		if c.Args().Len() == 0 || c.Args().First() == "init" {
			return ctx, nil
		}

		configPath, err := configPath(c)
		if err != nil {
			return nil, err
		}

		state, err := meta.NewState(configPath)
		if err != nil {
			return ctx, err
		}

		root := c.Root()
		if root.Metadata == nil {
			root.Metadata = make(map[string]any)
		}
		root.Metadata["state"] = state
		return ctx, nil
	},
	Action: func(ctx context.Context, c *opts.Command) error {
		if c.Args().Len() == 0 {
			return startTUI(c)
		}

		root := c.Root()
		state, ok := root.Metadata["state"].(*meta.State)
		if !ok || state == nil {
			return meta.ErrStaleState
		}

		fmt.Println(state.ConfigFile())

		return nil
	},
	Commands: []*opts.Command{
		{
			Name:   "init",
			Usage:  "Create a violet.toml configuration stub in the current directory",
			Action: cli.InitHandler,
		},
		{
			Name:   "upgrade",
			Usage:  "Upgrade Violet",
			Action: cli.UpgradeHandler,
		},
		{
			Name:   "server",
			Usage:  "Manage the Violet server",
			Action: nil,
			Commands: []*opts.Command{
				{
					Name:   "start",
					Usage:  "Start the Violet server",
					Action: cli.ServerStartHandler,
					Flags: []opts.Flag{
						&opts.StringFlag{
							Name:  "listen",
							Usage: "Address to serve the HTTP API on (default 127.0.0.1:8080); non-loopback requires VIOLET_SERVER_TOKEN",
						},
					},
				},
				{
					Name:   "status",
					Usage:  "Show the Violet server status",
					Action: cli.ServerStatusHandler,
				},
				{
					Name:   "stop",
					Usage:  "Stop the Violet server",
					Action: cli.ServerStopHandler,
				},
			},
		},
	},
}

func configPath(c *opts.Command) (string, error) {
	if path := c.String("config"); len(path) > 0 {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("%w: %w", meta.ErrConfigNotFound, err)
	}
	return filepath.Join(cwd, "violet.toml"), nil
}

// startTUI connects the TUI to a remote server (--server), to an in-process
// engine for the project's violet.toml, or runs it offline without a config.
func startTUI(c *opts.Command) error {
	if url := c.String("server"); url != "" {
		remote, err := client.NewHTTP(url, os.Getenv("VIOLET_SERVER_TOKEN"))
		if err != nil {
			return err
		}
		defer remote.Close()
		return ui.Start(remote)
	}

	path, err := configPath(c)
	if err != nil {
		return err
	}
	state, err := meta.NewState(path)
	// Only the implicit <cwd>/violet.toml may be absent; a missing explicit
	// --config path is a mistake and must not silently start offline.
	if errors.Is(err, meta.ErrConfigNotFound) && c.String("config") == "" {
		return ui.Start(nil)
	}
	if err != nil {
		return err
	}

	engine := server.NewEngine()
	if err := engine.Start(state); err != nil {
		return err
	}
	defer engine.Stop()
	local := server.NewLocalClient(engine)
	defer local.Close()
	return ui.Start(local)
}

func main() {
	if err := App.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
