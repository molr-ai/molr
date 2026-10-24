//go:build linux || darwin

package installation

import (
	"bufio"
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
	e.Options.Mode = "sandbox"
	e.Options.Preset = "permissive"
	e.Options.ProgramRoot = r.ProgramRoot
	e.Options.Port = 18789
	e.Options.PublicHost = "127.0.0.1"
	p, err := e.plan(&Package{Build: r.Build}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Record.Mode != "sandbox" || !p.Sandbox || p.Record.Manager != "" {
		t.Fatal("wrong Runtime mode")
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
	if !strings.Contains(args, "molr serve --mode sandbox -config") || !strings.Contains(args, "-token-file") {
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
	for _, args := range [][]string{{"--mode", "invalid"}, {"--resource-preset", "personal"}, {"--config", "server.json", "--resource-preset", "permissive"}, {"--mode", "sandbox", "--init-owner", "owner"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	o, err := parseOptions([]string{"--mode", "sandbox", "--resource-preset", "least-privilege"}, io.Discard)
	if err != nil || o.Mode != "sandbox" || o.Preset != "least-privilege" {
		t.Fatalf("%+v %v", o, err)
	}
}

func TestMaintenancePreservesModeAndRejectsSwitch(t *testing.T) {
	for _, mode := range []string{"server", "sandbox"} {
		t.Run(mode, func(t *testing.T) {
			e, r := fixture(t)
			r.Mode = mode
			cfg := map[string]any{"data_dir": r.DataDir, "sandbox": map[string]any{"enabled": false, "host_tools": true}, "http": map[string]any{"listen": "127.0.0.1:18790", "public_url": "http://127.0.0.1:18790", "tls_mode": "local"}}
			if mode == "sandbox" {
				cfg["http"] = map[string]any{"listen": "127.0.0.1:18790", "public_url": "https://127.0.0.1:18790", "tls_mode": "direct", "key_file": filepath.Join(r.DataDir, "sandbox.key"), "cert_file": filepath.Join(r.DataDir, "sandbox.crt")}
			}
			if err := os.WriteFile(filepath.Join(r.Paths.Config, "server.json"), jsonBytes(cfg), 0600); err != nil {
				t.Fatal(err)
			}
			for _, repair := range []bool{false, true} {
				e.Options.Repair = repair
				p, err := e.plan(&Package{Build: r.Build}, r)
				if err != nil {
					t.Fatal(err)
				}
				if p.Record.Mode != mode || p.Record.Binary() != "molr" {
					t.Fatal("maintenance changed service mode")
				}
			}
			e.Options.Mode = "sandbox"
			if mode == "sandbox" {
				e.Options.Mode = "server"
			}
			if _, err := e.plan(&Package{Build: r.Build}, r); err == nil {
				t.Fatal("maintenance changed mode")
			}
		})
	}
}

func TestFirstInstallResourcePresetDefaultsAndChoices(t *testing.T) {
	for _, mode := range []string{"server", "sandbox"} {
		for _, tc := range []struct {
			name, preset, answer string
			interactive, enabled bool
		}{
			{name: "unattended default", enabled: true},
			{name: "explicit personal", preset: "permissive", enabled: true},
			{name: "explicit least privilege", preset: "least-privilege"},
			{name: "interactive default", answer: "\n", interactive: true, enabled: true},
			{name: "interactive personal", answer: "p\n", interactive: true, enabled: true},
			{name: "interactive least privilege", answer: "l\n", interactive: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				e, r := fixture(t)
				e.Options.Name = "molr-preset-new"
				e.Options.Mode, e.Options.Preset = mode, tc.preset
				e.Options.ProgramRoot = r.ProgramRoot
				e.Options.Port, e.Options.PublicHost = 18789, "127.0.0.1"
				if tc.interactive {
					terminal, err := os.CreateTemp(t.TempDir(), "terminal")
					if err != nil {
						t.Fatal(err)
					}
					defer terminal.Close()
					e.UI.Terminal, e.UI.reader = terminal, bufio.NewReader(strings.NewReader(tc.answer))
				}
				plan, err := e.plan(&Package{Build: r.Build}, nil)
				if err != nil {
					t.Fatal(err)
				}
				sandbox := plan.Config["sandbox"].(map[string]any)
				if sandbox["enabled"] != tc.enabled || sandbox["allow_full_access"] != tc.enabled || sandbox["restrict_directories"] != !tc.enabled {
					t.Fatal("wrong preset", sandbox)
				}
				networks := sandbox["network_presets"].([]any)
				if (len(networks) > 0) != tc.enabled {
					t.Fatal("wrong network presets", networks)
				}
			})
		}
	}
}
