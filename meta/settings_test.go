package meta

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tristanisham/violet/ui/components"
)

func isolateGraphicConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	t.Setenv("AppData", root)
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGraphicSettingsDefaultsAndPersistence(t *testing.T) {
	root := isolateGraphicConfig(t)
	path := filepath.Join(root, "violet", "settings.json")
	defaults := NewGraphicSettings()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("default helper created a file", err)
	}
	settings, err := LoadGraphicSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Palette != "Violet" || !settings.MouseEnabled || len(settings.Palettes) != 6 {
		t.Fatalf("defaults = %+v", settings)
	}
	got, _ := json.Marshal(settings)
	want, _ := json.Marshal(defaults)
	if string(got) != string(want) {
		t.Fatal("defaults differ")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions", info.Mode())
	}
	settings.Palettes["Custom"] = components.PumpkinPalette
	settings.Palette = "Custom"
	settings.MouseEnabled = false
	if err := settings.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadGraphicSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Palette != "Custom" || loaded.MouseEnabled || len(loaded.Palettes) != 7 {
		t.Fatalf("roundtrip = %+v", loaded)
	}
	got, _ = json.Marshal(loaded.Palettes["Custom"])
	want, _ = json.Marshal(components.PumpkinPalette)
	if string(got) != string(want) {
		t.Fatal("custom palette lost")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files left behind", entries, err)
	}
}

func TestGraphicSettingsOverlayAndValidation(t *testing.T) {
	root := isolateGraphicConfig(t)
	path := filepath.Join(root, "violet", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{}`, `{"mouse_enabled":false}`, `{"palette":"Pumpkin"}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		settings, err := LoadGraphicSettings()
		if err != nil {
			t.Fatal(err)
		}
		if len(settings.Palettes) != 6 {
			t.Fatal("built-in palettes missing")
		}
		if data == `{"mouse_enabled":false}` && settings.MouseEnabled {
			t.Fatal("false ignored")
		}
		if data == `{"palette":"Pumpkin"}` && settings.Palette != "Pumpkin" {
			t.Fatal("selection ignored")
		}
	}
	for _, data := range []string{`{`, `{"mouse_enabled":"false"}`, `{"palette":"Unknown"}`, `{"palettes":null}`, `{"palettes":{"Custom":{"primary":"#123456"}}}`, `{"palettes":{"Custom":{"primary":"#ZZ0000"}}}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := LoadGraphicSettings(); got != nil || err == nil {
			t.Fatalf("accepted invalid settings: %s", data)
		}
		unchanged, err := os.ReadFile(path)
		if err != nil || string(unchanged) != data {
			t.Fatal("bad settings overwritten", err)
		}
	}
}

func TestGraphicSettingsSaveErrors(t *testing.T) {
	root := isolateGraphicConfig(t)
	settings := NewGraphicSettings()
	settings.Palettes["Broken"] = components.Palette{}
	if err := settings.Save(); err == nil {
		t.Fatal("accepted incomplete colors")
	}
	path := filepath.Join(root, "violet", "settings.json")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGraphicSettings(); err == nil {
		t.Fatal("expected directory read error")
	}
	if err := NewGraphicSettings().Save(); err == nil {
		t.Fatal("expected directory rename error")
	}
}

func TestNewStateConfig(t *testing.T) {
	isolateGraphicConfig(t)
	state, err := NewState("")
	if !errors.Is(err, ErrConfigNotFound) || state == nil || state.Config != nil {
		t.Fatalf("empty config = (%v, %v)", state, err)
	}
	state.GraphicSettings.MouseEnabled = false
	if err := state.GraphicSettings.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "violet.toml")
	original := []byte("container_socket = 'test.sock'\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	state, err = NewState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.ConfigFile() != path || state.Config.ContainerSock != "test.sock" || state.GraphicSettings.MouseEnabled {
		t.Fatalf("state = %+v", state)
	}
	if state, err := NewState(path + ".missing"); state != nil || !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("missing config = (%v, %v)", state, err)
	}
}
