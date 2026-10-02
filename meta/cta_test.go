package meta

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCtaFatalWritesPlainTextToNonTerminal(t *testing.T) {
	var out bytes.Buffer
	CtaFatal(&out, errors.New("config not found (x.toml)"))
	got := out.String()
	for _, want := range []string{"Error", "config not found (x.toml)", IssuesURL} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-terminal output contains ANSI escapes: %q", got)
	}
}
