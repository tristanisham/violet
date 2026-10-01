package meta

import (
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Model struct {
	Name    string `json:"name" toml:"name"`
	License string `json:"license" toml:"license"`
	URL     string `json:"url" toml:"url"` // TODO add decoding into an actual URL
}

type Family map[string]Model

type Config struct {
	ContainerSock string            `json:"container_sock" toml:"container_sock"`
	Models        map[string]Family `json:"models" toml:"models"`
}

func NewConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w (%s): %w", ErrConfigNotFound, path, err)
		}
		return nil, err
	}

	cfg := Config{
		Models: make(map[string]Family),
	}

	err = toml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("%w (%s): %w", ErrUnmarshalConfig, path, err)
	}

	return &cfg, nil
}
