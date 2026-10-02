package meta

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Model struct {
	Name    string `json:"name" toml:"name"`
	License string `json:"license" toml:"license"`
	URL     string `json:"url" toml:"url"` // TODO add decoding into an actual URL
}

type Family map[string]Model

type Config struct {
	ProjectDir    string            `json:"project_dir" toml:"project_dir"`
	ContainerSock string            `json:"container_socket" toml:"container_socket"`
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

	if cfg.ProjectDir == "" {
		cfg.ProjectDir = ".violet"
	}
	if !filepath.IsAbs(cfg.ProjectDir) {
		cfg.ProjectDir = filepath.Join(filepath.Dir(path), cfg.ProjectDir)
	}
	cfg.ProjectDir, err = filepath.Abs(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
