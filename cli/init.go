package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"charm.land/log/v2"
	"github.com/pelletier/go-toml/v2"
	"github.com/tristanisham/violet/meta"
	opts "github.com/urfave/cli/v3"
)

func InitHandler(ctx context.Context, c *opts.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	data, err := toml.Marshal(&meta.Config{})
	if err != nil {
		return err
	}

	// Note: the following section was AI generated.
	configPath := filepath.Join(cwd, "violet.toml")
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		log.Warnf("config file already present (%s)... skipping", configPath)
		return nil
	}
	if err != nil {
		return err
	}

	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}
