package installation

import (
	"fmt"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
)

func layout(name, home string, system bool, programRoot string) (installrecord.Paths, string, error) {
	instance, err := installrecord.Instance(name)
	if err != nil {
		return installrecord.Paths{}, "", err
	}
	if system {
		return installrecord.Paths{}, "", fmt.Errorf("macOS supports current-user installation only")
	}
	base := filepath.Join(home, "Library", "Application Support", "Molr")
	if programRoot == "" {
		programRoot = filepath.Join(base, "program")
	}
	envPath := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return filepath.Join(v, "molr")
		}
		return fallback
	}
	p := installrecord.Paths{Program: filepath.Join(programRoot, instance), Config: filepath.Join(envPath("XDG_CONFIG_HOME", filepath.Join(base, "config")), instance), Data: filepath.Join(envPath("XDG_DATA_HOME", filepath.Join(base, "data")), instance), Cache: filepath.Join(envPath("XDG_CACHE_HOME", filepath.Join(home, "Library", "Caches", "Molr")), instance), Unit: filepath.Join(home, "Library", "LaunchAgents", name+".plist")}
	for _, v := range []string{programRoot, p.Program, p.Config, p.Data, p.Cache, p.Unit} {
		if err := checkPath(v); err != nil {
			return p, "", err
		}
	}
	return p, programRoot, nil
}
