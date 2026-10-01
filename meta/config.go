package meta

import (
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	
}

func NewConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w (%s): %w", ErrConfigNotFound, path, err)
		}
		return nil, err
	}

	var cfg Config
	err = toml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("%w (%s): %w", ErrUnmarshalConfig, path, err)
	}

	return &cfg, nil
}