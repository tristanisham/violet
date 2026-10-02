package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tristanisham/violet/meta"
)

func TestExitCode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		code     int
		contains string
	}{
		{"success", nil, 0, ""},
		{"fatal", errors.New("boom"), 1, "boom"},
		{"wrapped fatal", fmt.Errorf("start: %w", meta.ErrConfigNotFound), 1, "start: config not found"},
		{"quiet", fmt.Errorf("%w: already reported", meta.ErrFailQuietly), 1, ""},
		{"quiet joined", errors.Join(errors.New("x"), meta.ErrFailQuietly), 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := exitCode(&out, tc.err); code != tc.code {
				t.Fatalf("exit code = %d, want %d", code, tc.code)
			}
			if tc.contains == "" && out.Len() != 0 {
				t.Fatalf("unexpected output %q", out.String())
			}
			if !strings.Contains(out.String(), tc.contains) {
				t.Fatalf("output %q is missing %q", out.String(), tc.contains)
			}
		})
	}
}
