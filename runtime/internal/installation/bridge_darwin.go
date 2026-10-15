package installation

import (
	"molr.ai/runtime/internal/maintenance"
	"os"
	"path/filepath"
)

// macOS maintenance is CLI-only; Runtime still protects its installed files.
type Maintenance struct {
	maintenance.Unavailable
	roots []string
}

func NewMaintenance(configPath string, external bool) *Maintenance {
	m := &Maintenance{Unavailable: maintenance.Unavailable{ExternalBrowser: external}}
	exe, err := os.Executable()
	if err != nil {
		return m
	}
	root := filepath.Dir(filepath.Dir(exe))
	r, err := readRecord(filepath.Join(root, "installation.json"), false)
	if err == nil && r.State == "installed" && r.Paths.Program == root && filepath.Join(r.Paths.Config, "server.json") == configPath {
		m.roots = []string{r.Paths.Program, r.Paths.Config, r.Paths.Cache}
	}
	return m
}
func (m *Maintenance) ProtectedRoots() []string { return m.roots }
