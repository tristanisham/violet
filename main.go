package main

import (
	"context"

	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/log/v2"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tristanisham/violet/cli"
	"github.com/tristanisham/violet/meta"
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
	},
	Before: func(ctx context.Context, c *opts.Command) (context.Context, error) {
		if c.Args().First() == "init" {
			return ctx, nil
		}

		configPath := c.String("config")
		if len(configPath) == 0 {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("%w: %w", meta.ErrConfigNotFound, err)
			}

			configPath = filepath.Join(cwd, "violet.toml")
		}

		settings, err := meta.NewSettings(configPath)
		if err != nil {
			return ctx, err
		}

		root := c.Root()
		if root.Metadata == nil {
			root.Metadata = make(map[string]any)
		}
		root.Metadata["settings"] = settings
		return ctx, nil
	},
	Action: func(ctx context.Context, c *opts.Command) error {
		root := c.Root()
		if root.Metadata == nil {
			// TODO come up with a better error type
			return fmt.Errorf("internal data missing")
		}

		fmt.Println(root.Metadata["settings"].(*meta.Settings).ConfigFile())

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

func main() {
	if err := App.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
