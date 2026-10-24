//go:build linux

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/sandboxidentity"
)

func loadContainerConfig(t *testing.T, path string) config.Config {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("configuration must stay private", err)
	}
	return cfg
}
func TestContainerServerFirstRunPreservesConfigurationAndAccount(t *testing.T) {
	root := t.TempDir()
	path, err := prepareContainer(root, "server", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadContainerConfig(t, path)
	if cfg.HTTP.PublicURL != "http://127.0.0.1:8787" || cfg.HTTP.Listen != "0.0.0.0:8787" || !cfg.Sandbox.Enabled || !cfg.Sandbox.AllowFullAccess || cfg.Sandbox.RestrictDirectories || len(cfg.Sandbox.NetworkPresets) != 3 {
		t.Fatal(cfg)
	}
	if _, err = invokeCLI(t, t.Context(), []string{"bootstrap", "-config", path}, ""); err != nil {
		t.Fatal(err)
	}
	password, err := os.ReadFile(filepath.Join(root, "initial-password"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]any
	json.Unmarshal(data, &doc)
	doc["sandbox"] = map[string]any{"enabled": false, "allow_full_access": false}
	doc["updates"] = map[string]any{"auto_check": false}
	doc["http"].(map[string]any)["allowed_origins"] = []string{cfg.HTTP.PublicURL, "http://127.0.0.2:8787"}
	data, _ = json.Marshal(doc)
	os.WriteFile(path, data, 0600)
	_, err = prepareContainer(root, "server", "http://192.168.1.20:9898", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg = loadContainerConfig(t, path)
	if cfg.HTTP.Listen != "0.0.0.0:9898" || cfg.HTTP.PublicURL != "http://192.168.1.20:9898" || cfg.Sandbox.Enabled || cfg.Sandbox.AllowFullAccess || cfg.Updates.AutoCheck || len(cfg.HTTP.AllowedOrigins) != 2 {
		t.Fatal(cfg)
	}
	before, _ := os.ReadFile(path)
	_, err = prepareContainer(root, "server", "", "")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("restart rewrote customized configuration")
	}
	if _, err = invokeCLI(t, t.Context(), []string{"bootstrap", "-config", path}, ""); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(filepath.Join(root, "initial-password"))
	if !bytes.Equal(password, after) {
		t.Fatal("restart changed password")
	}
}
func TestContainerSandboxGeneratesPrivateStableCredentials(t *testing.T) {
	root := t.TempDir()
	path, err := prepareContainer(root, "sandbox", "https://127.0.0.1:9443", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadContainerConfig(t, path)
	if cfg.DataDir != filepath.Join(root, "private") || !cfg.Sandbox.Enabled || !cfg.Sandbox.AllowFullAccess || cfg.Sandbox.RestrictDirectories || len(cfg.Sandbox.NetworkPresets) != 3 {
		t.Fatal(cfg)
	}
	key, err := tls.LoadX509KeyPair(cfg.HTTP.CertFile, cfg.HTTP.KeyFile)
	if err != nil || len(key.Certificate) != 1 {
		t.Fatal(err)
	}
	saved := map[string][]byte{}
	for _, path := range []string{cfg.HTTP.CertFile, cfg.HTTP.KeyFile, filepath.Join(cfg.DataDir, "sandbox-token")} {
		value, err := containerFile(path)
		if err != nil {
			t.Fatal(err)
		}
		saved[path] = value
	}
	if len(strings.TrimSpace(string(saved[filepath.Join(cfg.DataDir, "sandbox-token")]))) != 64 {
		t.Fatal("unexpected token entropy")
	}
	_, err = prepareContainer(root, "sandbox", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for path, value := range saved {
		after, _ := os.ReadFile(path)
		if !bytes.Equal(value, after) {
			t.Fatal("node identity changed", path)
		}
	}
	// Resume an interrupted first start using the key already written.
	os.Remove(cfg.HTTP.CertFile)
	_, err = prepareContainer(root, "sandbox", "", "")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(cfg.HTTP.KeyFile)
	if !bytes.Equal(after, saved[cfg.HTTP.KeyFile]) {
		t.Fatal("replaced key")
	}
	os.Remove(cfg.HTTP.KeyFile)
	if _, err = prepareContainer(root, "sandbox", "", ""); err == nil {
		t.Fatal("replaced incomplete existing identity")
	}
}
func TestContainerRejectsInvalidConfigAndLinkedSecrets(t *testing.T) {
	for _, value := range []string{"http://example.com:8787", "http://192.168.1.2:8787/path", "http://user:password@127.0.0.1:8787", "https://127.0.0.1:8787"} {
		if _, err := prepareContainer(t.TempDir(), "server", value, ""); err == nil {
			t.Fatal("invalid address accepted", value)
		}
	}
	if _, err := prepareContainer(t.TempDir(), "other", "", ""); err == nil {
		t.Fatal("invalid mode accepted")
	}
	root := t.TempDir()
	path := filepath.Join(root, "server.json")
	os.WriteFile(path, []byte("invalid configuration"), 0600)
	if _, err := prepareContainer(root, "server", "", ""); err == nil {
		t.Fatal("invalid configuration replaced")
	}
	value, _ := os.ReadFile(path)
	if string(value) != "invalid configuration" {
		t.Fatal("configuration overwritten")
	}
	root = t.TempDir()
	path, err := prepareContainer(root, "sandbox", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadContainerConfig(t, path)
	tokenPath := filepath.Join(cfg.DataDir, "sandbox-token")
	outside := filepath.Join(t.TempDir(), "token")
	os.WriteFile(outside, []byte(strings.Repeat("a", 64)), 0600)
	os.Remove(tokenPath)
	if err = os.Symlink(outside, tokenPath); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err = prepareContainer(root, "sandbox", "", ""); err == nil {
		t.Fatal("linked token accepted")
	}
}

func TestContainerConnectionCodeExportPreservesIdentity(t *testing.T) {
	root := t.TempDir()
	path, err := prepareContainer(root, "sandbox", "", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadContainerConfig(t, path)
	if !cfg.HTTP.AutoRenewCertificate {
		t.Fatal("managed certificate renewal is disabled")
	}
	var output, diagnostics bytes.Buffer
	if err := run(t.Context(), []string{"connection-code", "--config", path}, strings.NewReader(""), &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	code := strings.TrimSpace(output.String())
	if !strings.HasPrefix(code, "molr-sandbox-v1.") || diagnostics.Len() != 0 {
		t.Fatal("export failed or leaked into diagnostics")
	}
	pin, token, err := sandboxidentity.Decode(code)
	if err != nil || pin == "" || len(token.Reveal()) != 64 {
		t.Fatal("invalid exported code", err)
	}
	output.Reset()
	if err := run(t.Context(), []string{"connection-code", "--config", path}, strings.NewReader(""), &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != code {
		t.Fatal("re-export changed pairing identity")
	}
	tokenPath := filepath.Join(cfg.DataDir, "sandbox-token")
	if err := os.Chmod(tokenPath, 0644); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := exportSandboxConnectionCode(cfg, "", &output); err == nil || output.Len() != 0 {
		t.Fatal("nonprivate token was exported")
	}
}

func TestContainerResourcePreset(t *testing.T) {
	for _, mode := range []string{"server", "sandbox"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path, err := prepareContainer(root, mode, "", "least-privilege")
			if err != nil {
				t.Fatal(err)
			}
			cfg := loadContainerConfig(t, path)
			if cfg.Sandbox.Enabled || cfg.Sandbox.AllowFullAccess || !cfg.Sandbox.RestrictDirectories || len(cfg.Sandbox.NetworkPresets) != 0 {
				t.Fatal("least privilege expanded incorrectly")
			}
			before, _ := os.ReadFile(path)
			if _, err := prepareContainer(root, mode, "", "permissive"); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("initial preset changed existing settings")
			}
		})
	}
	if _, err := prepareContainer(t.TempDir(), "server", "", "invalid"); err == nil {
		t.Fatal("invalid initial preset accepted")
	}
}
