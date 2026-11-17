package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/execenv"
	"molr.ai/runtime/internal/platform"
)

func TestEnvironmentCLIForServerAndStandaloneSandbox(t *testing.T) {
	for _, name := range []string{"server", "sandbox"} {
		t.Run(name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults(filepath.Join(root, "data"))
			cfg.HTTP.TLSMode = config.TrustedProxy
			cfg.HTTP.PublicURL = "https://runtime.example"
			cfg.HTTP.TrustedProxies = []string{"127.0.0.1/32"}
			if err := os.Mkdir(cfg.DataDir, 0700); err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(root, name+".json")
			data, _ := json.Marshal(cfg)
			if err := os.WriteFile(cfgPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", root)
			t.Setenv("CUSTOM", "private $(literal) value")
			out, err := invokeCLI(t, t.Context(), []string{"environment", "--config", cfgPath, "--dry-run"}, "")
			if err != nil || !strings.Contains(out, "PATH") {
				t.Fatal(out, err)
			}
			entries, _ := os.ReadDir(cfg.DataDir)
			if len(entries) != 0 {
				t.Fatal("dry run wrote files")
			}
			// An active Runtime holds this lock. Environment updates must not open
			// its database, bind a listener, or acquire the server lock.
			lock, err := platform.NewLocker().Acquire(t.Context(), cfg.DataDir)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			out, err = invokeCLI(t, t.Context(), []string{"environment", "--config", cfgPath, "--env", "PATH,CUSTOM"}, "")
			if err != nil || strings.Contains(out, "private $(literal) value") {
				t.Fatal(out, err)
			}
			t.Setenv("PATH", filepath.Join(root, "next"))
			if _, err = invokeCLI(t, t.Context(), []string{"environment", "--config", cfgPath}, ""); err != nil {
				t.Fatal(err)
			}
			values, err := execenv.Read(cfg.DataDir)
			if err != nil || values["CUSTOM"] != "private $(literal) value" || values["PATH"] != filepath.Join(root, "next") {
				t.Fatal(values, err)
			}
			if _, err := os.Stat(filepath.Join(cfg.DataDir, "runtime.db")); !os.IsNotExist(err) {
				t.Fatal("environment command opened database", err)
			}
		})
	}
}

func TestEnvironmentCLIRequiresExplicitConfig(t *testing.T) {
	for _, args := range [][]string{{"environment"}, {"environment", "--env", "PATH"}, {"environment", "--config", "missing", "unexpected"}} {
		if _, err := invokeCLI(t, t.Context(), args, ""); err == nil {
			t.Fatal("accepted ambiguous instance", args)
		}
	}
	if _, err := invokeCLI(t, t.Context(), []string{"environment", "--help"}, ""); err != nil {
		t.Fatal(err)
	}
}
