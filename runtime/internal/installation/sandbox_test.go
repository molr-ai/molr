//go:build linux || darwin

package installation

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxInstallPlanAndSecrets(t *testing.T) {
	e, r := fixture(t)
	e.Options.Name = "molr-node-new"
	e.Options.Target = "sandbox"
	e.Options.Preset = "permissive"
	e.Options.ProgramRoot = r.ProgramRoot
	e.Options.Port = 18789
	e.Options.PublicHost = "127.0.0.1"
	p, err := e.plan(&Package{Build: r.Build}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Record.Target != "sandbox" || !p.Sandbox || p.Record.Manager != "" {
		t.Fatal("wrong installation target")
	}
	h := p.Config["http"].(map[string]any)
	if h["tls_mode"] != "direct" || !strings.HasPrefix(h["public_url"].(string), "https://") {
		t.Fatal("sandbox TLS not configured")
	}
	if err = os.MkdirAll(p.Record.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = e.prepareSandboxSecrets(p.Record, p.Config); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(p.Record.DataDir, "sandbox-token")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(tokenPath)
	if st.Mode().Perm() != 0600 || len(strings.TrimSpace(string(token))) < 32 {
		t.Fatal("unsafe service token")
	}
	pair, err := tls.LoadX509KeyPair(h["cert_file"].(string), h["key_file"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.prepareSandboxSecrets(p.Record, p.Config); err != nil {
		t.Fatal(err)
	}
	next, _ := os.ReadFile(tokenPath)
	if !bytes.Equal(token, next) {
		t.Fatal("existing token rotated")
	}
	pair2, _ := tls.LoadX509KeyPair(h["cert_file"].(string), h["key_file"].(string))
	if !bytes.Equal(pair.Certificate[0], pair2.Certificate[0]) {
		t.Fatal("certificate rotated")
	}
	if err = e.sandboxInstructions(p.Record, p.Config); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.UI.Out.(*bytes.Buffer).String(), strings.TrimSpace(string(token))) {
		t.Fatal("secret printed in receipt")
	}
	args := strings.Join(p.Record.ServiceArgs(), " ")
	if !strings.Contains(args, "molr-sandbox -config") || !strings.Contains(args, "-token-file") || strings.Contains(args, " serve ") {
		t.Fatal(args)
	}
	if _, err = os.Stat(filepath.Join(p.Record.DataDir, "runtime.db")); !os.IsNotExist(err) {
		t.Fatal("sandbox initialized Runtime data")
	}
}
func TestPresetCannotOverwriteExistingConfiguration(t *testing.T) {
	e, r := fixture(t)
	cfg := map[string]any{"data_dir": r.DataDir, "sandbox": map[string]any{"enabled": false, "restrict_directories": true, "host_tools": true}, "http": map[string]any{"listen": "127.0.0.1:18790", "public_url": "http://127.0.0.1:18790", "tls_mode": "local"}}
	path := filepath.Join(r.Paths.Config, "server.json")
	before := jsonBytes(cfg)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	e.Options.Preset = "permissive"
	plan, err := e.plan(&Package{Build: r.Build}, r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, jsonBytes(plan.Config)) {
		t.Fatal("upgrade reapplied initial preset")
	}
	r.State = "uninstalled"
	plan, err = e.plan(&Package{Build: r.Build}, r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, jsonBytes(plan.Config)) {
		t.Fatal("reinstall reapplied initial preset")
	}
	r.State = "installed"
	e.Options.Command = "configure"
	e.Options.DryRun = true
	if err := e.configureResources(r); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("preview changed configuration")
	}
	var decoded map[string]any
	if err := json.Unmarshal(after, &decoded); err != nil {
		t.Fatal(err)
	}
}
func TestPresetOptionsRejectAmbiguity(t *testing.T) {
	for _, args := range [][]string{{"--target", "invalid"}, {"--resource-preset", "personal"}, {"--config", "server.json", "--resource-preset", "permissive"}, {"--target", "sandbox", "--init-owner", "owner"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	o, err := parseOptions([]string{"--target", "sandbox", "--resource-preset", "least-privilege"}, io.Discard)
	if err != nil || o.Target != "sandbox" || o.Preset != "least-privilege" {
		t.Fatalf("%+v %v", o, err)
	}
}
