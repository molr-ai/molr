package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTUIRoutesBeforeServerConfiguration(t *testing.T) {
	var output, diagnostics bytes.Buffer
	err := run(t.Context(), []string{"tui", "--help"}, strings.NewReader(""), &output, &diagnostics)
	if err != nil || !strings.Contains(diagnostics.String(), "-demo") {
		t.Fatalf("help: %v %s", err, diagnostics.String())
	}
	err = run(t.Context(), []string{"tui", "--demo"}, strings.NewReader(""), &output, &diagnostics)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("TUI loaded server config: %v", err)
	}
}
