package meta

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewConfigProjectDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	if configDir == cwd {
		t.Fatal("config directory must be outside the working directory")
	}
	absoluteDir := configDir + string(filepath.Separator) + "unused" + string(filepath.Separator) + ".." + string(filepath.Separator) + "absolute"

	tests := []struct {
		name       string
		setting    string
		projectDir string
	}{
		{
			name:       "missing",
			projectDir: filepath.Join(configDir, ".violet"),
		},
		{
			name:       "empty",
			setting:    "project_dir = ''\n",
			projectDir: filepath.Join(configDir, ".violet"),
		},
		{
			name:       "relative",
			setting:    "project_dir = 'unused/../relative'\n",
			projectDir: filepath.Join(configDir, "relative"),
		},
		{
			name:       "absolute",
			setting:    fmt.Sprintf("project_dir = '%s'\n", absoluteDir),
			projectDir: filepath.Join(configDir, "absolute"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(configDir, "violet.toml")
			data := tt.setting + `container_socket = 'test.sock'
[models.test.small]
name = 'test-model'
license = 'MIT'
url = 'https://example.com/model'
`
			if err := os.WriteFile(configPath, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			relativePath, err := filepath.Rel(cwd, configPath)
			if err != nil {
				t.Fatal(err)
			}

			for _, path := range []string{configPath, relativePath} {
				cfg, err := NewConfig(path)
				if err != nil {
					t.Fatalf("NewConfig(%q): %v", path, err)
				}
				if cfg.ProjectDir != tt.projectDir {
					t.Errorf("NewConfig(%q).ProjectDir = %q, want %q", path, cfg.ProjectDir, tt.projectDir)
				}
				if cfg.ContainerSock != "test.sock" {
					t.Errorf("ContainerSock = %q, want test.sock", cfg.ContainerSock)
				}
				wantModel := Model{Name: "test-model", License: "MIT", URL: "https://example.com/model"}
				if got := cfg.Models["test"]["small"]; got != wantModel {
					t.Errorf("model = %+v, want %+v", got, wantModel)
				}
			}

			entries, err := os.ReadDir(configDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "violet.toml" {
				t.Errorf("config loading created unexpected filesystem entries: %v", entries)
			}
		})
	}
}

func TestNewConfigErrors(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "violet.toml")
	cfg, err := NewConfig(configPath)
	if cfg != nil || !errors.Is(err, ErrConfigNotFound) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing config: got (%v, %v), want nil and wrapped ErrConfigNotFound/os.ErrNotExist", cfg, err)
	}
	if !strings.Contains(err.Error(), configPath) {
		t.Errorf("missing config error does not include path: %v", err)
	}

	if err := os.WriteFile(configPath, []byte("project_dir = ["), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = NewConfig(configPath)
	if cfg != nil || !errors.Is(err, ErrUnmarshalConfig) {
		t.Fatalf("malformed config: got (%v, %v), want nil and wrapped ErrUnmarshalConfig", cfg, err)
	}
	if !strings.Contains(err.Error(), configPath) {
		t.Errorf("unmarshal error does not include path: %v", err)
	}
}
