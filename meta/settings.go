package meta

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"charm.land/log/v2"
	"github.com/tristanisham/violet/ui/components"
)

type State struct {
	configFilepath  string
	Config          *Config
	GraphicSettings GraphicSettings
}

type GraphicSettings struct {
	Palette      string                        `json:"palette" toml:"palette"`
	MouseEnabled bool                          `json:"mouse_enabled" toml:"mouse_enabled"`
	Palettes     map[string]components.Palette `json:"palettes" toml:"palettes"`
}

func NewGraphicSettings() *GraphicSettings {
	return &GraphicSettings{
		Palette:      "Violet",
		MouseEnabled: true,
		Palettes: map[string]components.Palette{
			"Violet":  components.VioletPalette,
			"Misty":   components.MistyPalette,
			"Forest":  components.ForestPalette,
			"Slate":   components.SlatePalette,
			"Pumpkin": components.PumpkinPalette,
			"Autumn":  components.AutumnPalette,
		},
	}
}

func graphicSettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "violet", "settings.json"), nil
}

func LoadGraphicSettings() (*GraphicSettings, error) {
	path, err := graphicSettingsPath()
	if err != nil {
		return nil, err
	}
	settings := NewGraphicSettings()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := settings.Save(); err != nil {
			return nil, err
		}
		return settings, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, settings); err != nil {
		return nil, fmt.Errorf("decode graphic settings (%s): %w", path, err)
	}
	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("graphic settings (%s): %w", path, err)
	}
	return settings, nil
}

func (s GraphicSettings) Validate() error {
	if _, ok := s.Palettes[s.Palette]; !ok {
		return fmt.Errorf("selected palette %q does not exist", s.Palette)
	}
	for name, palette := range s.Palettes {
		if name == "" {
			return fmt.Errorf("palette name must not be empty")
		}
		if _, err := json.Marshal(palette); err != nil {
			return fmt.Errorf("palette %q: %w", name, err)
		}
	}
	return nil
}

func (s *GraphicSettings) Save() error {
	if err := s.Validate(); err != nil {
		return err
	}
	path, err := graphicSettingsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func NewState(configPath string) (*State, error) {
	graphics, err := LoadGraphicSettings()
	if err != nil {
		return nil, err
	}
	if len(configPath) == 0 {
		return &State{
			configFilepath:  "",
			Config:          nil,
			GraphicSettings: *graphics,
		}, ErrConfigNotFound
	}
	config, err := NewConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &State{
		configFilepath:  configPath,
		Config:          config,
		GraphicSettings: *graphics,
	}, nil
}

func (s State) ConfigFile() string {
	path, err := filepath.Abs(s.configFilepath)
	if err != nil {
		log.Debugf("failed to create absolute path: %s", err)
		return s.configFilepath
	}
	return path
}
