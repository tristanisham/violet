package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tristanisham/violet/meta"
)

func TestInitExistingConfigFailsQuietly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := InitHandler(context.Background(), nil); err != nil {
		t.Fatalf("first init = %v", err)
	}
	if _, err := meta.NewConfig(filepath.Join(dir, "violet.toml")); err != nil {
		t.Fatalf("init stub does not load: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "violet.toml"))
	if err != nil {
		t.Fatal(err)
	}
	err = InitHandler(context.Background(), nil)
	if !errors.Is(err, meta.ErrFailQuietly) || !errors.Is(err, os.ErrExist) {
		t.Fatalf("second init = %v, want ErrFailQuietly wrapping ErrExist", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "violet.toml"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("existing config changed: %v", err)
	}
}
