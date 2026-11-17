package installationwindows

import (
	"io"
	"molr.ai/runtime/internal/execenv"
	"path/filepath"
	"testing"
)

func TestWindowsInstallationCapturesEnvironment(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "data")
	selected, err := execenv.Capture("", []string{"Path=" + root, "OTHER=not-selected"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeEnvironment(t.Context(), directory, selected); err != nil {
		t.Fatal(err)
	}
	got, err := execenv.Read(directory)
	if err != nil || len(got) != 1 || got["PATH"] != root {
		t.Fatal(got, err)
	}
	if err := Run(t.Context(), []string{"environment", "--name", "molr-environment", "--program-root", root}, io.Discard, io.Discard); err == nil {
		t.Fatal("installer exposed environment command")
	}
}
