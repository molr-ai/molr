package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIFileLoggingUsesConfiguredRetention(t *testing.T) {
	cfg, configPath := cliConfig(t)
	cfg.Logging.MaxFileBytes = 1024
	cfg.Logging.BackupFiles = 1
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(configPath), "runtime.log")
	previousRecord := strings.Repeat("x", 1023) + "\n"
	if err := os.WriteFile(path, []byte(previousRecord), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".2", []byte("expired"), 0600); err != nil {
		t.Fatal(err)
	}
	previous := log.Writer()
	var diagnostics bytes.Buffer
	// Missing sandbox credentials fail before serving. The error record must
	// use the configured service sink and rotate the full file.
	err = run(t.Context(), []string{"serve", "--mode", "sandbox", "--config", configPath, "--log-file", path}, strings.NewReader(""), io.Discard, &diagnostics)
	if err == nil {
		t.Fatal("expected missing sandbox credentials")
	}
	if log.Writer() != previous {
		t.Fatal("default logger output not restored")
	}
	current, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(current), "molr:") || len(current) > 1024 {
		t.Fatalf("error not in bounded log: %q, %v", current, err)
	}
	archive, err := os.ReadFile(path + ".1")
	if err != nil || string(archive) != previousRecord {
		t.Fatal("configured rotation lost previous record", err)
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatal("configured retention was not applied")
	}
	if diagnostics.Len() != 0 {
		t.Fatal("service diagnostics bypassed the file sink")
	}
}
