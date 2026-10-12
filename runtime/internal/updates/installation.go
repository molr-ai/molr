package updates

import (
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
)

// Installation is administrator-only metadata for the installed maintenance tool.
type Installation struct {
	Name      string `json:"name"`
	System    bool   `json:"system"`
	SetupPath string `json:"setup_path"`
	Source    string `json:"source"`
}

func ReadInstallation(configPath, _ string) *Installation {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	return readInstallation(executable, configPath)
}
func readInstallation(executable, configPath string) *Installation {
	if configPath == "" {
		return nil
	}
	root := filepath.Dir(filepath.Dir(executable))
	r, err := installrecord.Read(filepath.Join(root, "installation.json"))
	if err != nil || r.State != "installed" || r.Paths.Program != root || filepath.Join(r.Paths.Config, "server.json") != configPath {
		return nil
	}
	return &Installation{Name: r.Name, System: r.System, SetupPath: filepath.Join(root, "bin/molr-setup"), Source: r.UpdateSource()}
}
