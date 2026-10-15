//go:build linux

package installation

import (
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
)

func layout(name, home string, system bool, programRoot string) (installrecord.Paths, string, error) {
	instance, err := installrecord.Instance(name)
	if err != nil {
		return installrecord.Paths{}, "", err
	}
	envPath := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	var p installrecord.Paths
	if system {
		if programRoot == "" {
			programRoot = "/opt/molr"
		}
		p = installrecord.Paths{Program: filepath.Join(programRoot, instance), Config: filepath.Join("/etc/molr", instance), Data: filepath.Join("/var/lib/molr", instance), Cache: filepath.Join("/var/cache/molr", instance), Unit: filepath.Join("/etc/systemd/system", name+".service")}
	} else {
		if programRoot == "" {
			programRoot = filepath.Join(home, ".local/lib/molr")
		}
		c := envPath("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		d := envPath("XDG_DATA_HOME", filepath.Join(home, ".local/share"))
		k := envPath("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
		p = installrecord.Paths{Program: filepath.Join(programRoot, instance), Config: filepath.Join(c, "molr", instance), Data: filepath.Join(d, "molr", instance), Cache: filepath.Join(k, "molr", instance), Unit: filepath.Join(c, "systemd/user", name+".service")}
	}
	for _, v := range []string{programRoot, p.Program, p.Config, p.Data, p.Cache, p.Unit} {
		if err = checkPath(v); err != nil {
			return p, "", err
		}
	}
	return p, programRoot, nil
}
