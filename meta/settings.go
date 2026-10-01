package meta

import (
	"path/filepath"

	"charm.land/log/v2"
)

type Settings struct {
	configFilepath string
	Config         *Config
}

func NewSettings(configPath string) (*Settings, error) {

	if len(configPath) == 0 {
		return &Settings{
			configFilepath: "",
			Config: nil,
		}, ErrConfigNotFound
	}
	// Checks to see if it exists and can be read and unmarshalled.
	config, err := NewConfig(configPath)
	if err != nil {
		return nil, err
	}

	set := &Settings{
		configFilepath: configPath,
		Config: config,
	}

	return set, nil
}
func (s Settings) ConfigFile() string {
	path, err := filepath.Abs(s.configFilepath)
	if err != nil {
		log.Debugf("failed to create absolute path: %s", err)
		return s.configFilepath
	}

	return path
}