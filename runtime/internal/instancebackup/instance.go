package instancebackup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/storage/sqlite"
)

// Capture requires an offline instance and an empty staging directory. It never
// traverses directory resources or contacts remote sandbox nodes.
func Capture(ctx context.Context, stage string, r installrecord.Record) (*Manifest, error) {
	if err := platform.PrivateDirectory(stage); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(r.Paths.Config, "server.json"))
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	for _, source := range []string{r.DataDir, r.Paths.Config, r.Paths.Program, cfg.Sandbox.WorkRoot} {
		if source == "" {
			continue
		}
		rel, err := filepath.Rel(source, stage)
		if err == nil && (rel == "." || filepath.IsLocal(rel)) {
			return nil, fmt.Errorf("staging directory must be outside backup sources")
		}
	}
	if cfg.DataDir != r.DataDir {
		return nil, fmt.Errorf("installation data directory differs from configuration")
	}
	m := &Manifest{Mode: r.Mode, Build: r.Build, DataDir: r.DataDir, ConfigDir: r.Paths.Config, WorkRoot: cfg.Sandbox.WorkRoot, References: map[string]string{}}
	if err = json.Unmarshal(b, &m.Config); err != nil {
		return nil, err
	}
	for _, d := range cfg.Sandbox.Directories {
		m.Excluded = append(m.Excluded, d.Path)
	}
	dbName := "runtime.db"
	if r.Mode == "sandbox" {
		dbName = "sandbox.db"
	}
	if _, err = os.Stat(filepath.Join(r.DataDir, dbName)); err != nil {
		return nil, err
	}
	// Open also takes the database lock; Close checkpoints a crash-left WAL.
	db, err := sqlite.Open(filepath.Join(r.DataDir, dbName))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	m.Schema, err = db.Schema(ctx)
	if err != nil {
		return nil, err
	}
	if err = db.Check(ctx); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(stage, "data"), 0700); err != nil {
		return nil, err
	}
	snapshot, err := db.Backup(ctx)
	if err != nil {
		return nil, err
	}
	snapshotPath := filepath.Join(r.DataDir, "backups", dbName, string(snapshot.ID)+".db")
	defer os.Remove(snapshotPath)
	defer os.Remove(strings.TrimSuffix(snapshotPath, ".db") + ".json")
	if err = Add(ctx, stage, snapshotPath, "data/"+dbName, nil); err != nil {
		return nil, err
	}
	filter := func(rel string) bool {
		first, _, _ := strings.Cut(rel, "/")
		if rel == "sandbox/runtime" {
			return false
		}
		if rel == dbName {
			return false
		}
		if first == "backups" || first == "server.lock" || first == ".runtime.db.instance" || first == ".sandbox.db.instance" || first == "runtime.db-wal" || first == "runtime.db-shm" || first == "sandbox.db-wal" || first == "sandbox.db-shm" {
			return false
		}
		if r.Mode == "sandbox" {
			return rel == "." || first == "sandbox.db" || first == "keys" || first == "sandbox-token"
		}
		return true
	}
	if err = Add(ctx, stage, r.DataDir, "data", filter); err != nil {
		return nil, err
	}
	if err = filepath.WalkDir(r.Paths.Config, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("configuration must not contain symlinks: %s", p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err = Add(ctx, stage, r.Paths.Config, "config", nil); err != nil {
		return nil, err
	}
	if r.Mode == "server" && cfg.Sandbox.WorkRoot != "" {
		if err = Add(ctx, stage, cfg.Sandbox.WorkRoot, "work", nil); err != nil {
			return nil, fmt.Errorf("managed workspace: %w", err)
		}
	}
	references := map[string]string{"http.cert_file": cfg.HTTP.CertFile, "http.key_file": cfg.HTTP.KeyFile, "pricing.rules_file": cfg.Pricing.RulesFile, "pricing.exchange_rates_file": cfg.Pricing.ExchangeRatesFile}
	for key, p := range references {
		if p == "" {
			continue
		}
		name := "config/references/" + strings.ReplaceAll(key, ".", "-")
		if err = os.MkdirAll(filepath.Join(stage, "config", "references"), 0700); err != nil {
			return nil, err
		}
		if err = os.Remove(filepath.Join(stage, filepath.FromSlash(name))); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err = Add(ctx, stage, p, name, nil); err != nil {
			return nil, err
		}
		m.References[key] = name
	}
	if err = os.MkdirAll(filepath.Join(stage, "program", "bin"), 0700); err != nil {
		return nil, err
	}
	suffix := ""
	if r.Build.OS == "windows" {
		suffix = ".exe"
	}
	for _, name := range []string{"bin/molr" + suffix, "bin/molr-setup" + suffix, "release.json", "LICENSE", "THIRD_PARTY_NOTICES.md"} {
		if err = Add(ctx, stage, filepath.Join(r.Paths.Program, filepath.FromSlash(name)), "program/"+name, nil); err != nil {
			return nil, err
		}
	}
	if err = Seal(ctx, stage, m); err != nil {
		return nil, err
	}
	return m, nil
}

// Configuration relocates managed paths without changing remote endpoints.
func Configuration(m *Manifest, dataDir, configDir, workRoot string) (map[string]any, []string, error) {
	b, _ := json.Marshal(m.Config)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, nil, err
	}
	out["data_dir"] = dataDir
	s, ok := out["sandbox"].(map[string]any)
	if !ok {
		s = map[string]any{}
		out["sandbox"] = s
	}
	s["work_root"] = workRoot
	var notes []string
	if m.Build.OS != runtime.GOOS || m.Build.Arch != runtime.GOARCH {
		// Installed browser executables must be prepared for the target platform.
		out["browser"] = map[string]any{}
	}
	if m.Build.OS != runtime.GOOS {
		defaults := config.Defaults(dataDir)
		raw, _ := json.Marshal(defaults.Sandbox)
		var native map[string]any
		json.Unmarshal(raw, &native)
		keys := []string{"bubblewrap", "rootfs", "cgroup_root", "tool_directories", "gpu", "workspace_project_quotas", "authorization_ceilings", "memory_bytes", "processes", "cpu_percent", "temp_bytes", "max_file_bytes", "network_bytes", "network_connections", "min_free_disk_bytes", "networks", "network_presets"}
		for _, key := range keys {
			delete(s, key)
			if v, ok := native[key]; ok {
				s[key] = v
			}
		}
		var dirs []any
		if entries, ok := s["directories"].([]any); ok {
			for _, v := range entries {
				d, ok := v.(map[string]any)
				if !ok {
					continue
				}
				p, _ := d["path"].(string)
				if filepath.IsAbs(p) && filepath.Clean(p) == p {
					dirs = append(dirs, v)
				} else {
					notes = append(notes, "External directory unavailable: "+p)
				}
			}
		}
		s["directories"] = dirs
		notes = append(notes, "Platform-specific sandbox settings reset to native defaults; inspect capabilities before use.")
	}
	for key, name := range m.References {
		parts := strings.Split(key, ".")
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("invalid configuration reference")
		}
		if key != "http.cert_file" && key != "http.key_file" && key != "pricing.rules_file" && key != "pricing.exchange_rates_file" {
			return nil, nil, fmt.Errorf("unsupported configuration reference")
		}
		group, ok := out[parts[0]].(map[string]any)
		if !ok {
			group = map[string]any{}
			out[parts[0]] = group
		}
		target := filepath.Join(configDir, "references", filepath.Base(name))
		if m.Mode == "sandbox" && (key == "http.key_file" || key == "http.cert_file") {
			target = filepath.Join(dataDir, "restored-"+parts[1])
		}
		group[parts[1]] = target
	}
	for _, p := range m.Excluded {
		if _, err := os.Stat(p); err != nil {
			notes = append(notes, "External directory unavailable: "+p)
		}
	}
	raw, _ := json.Marshal(out)
	if _, err := config.Load(strings.NewReader(string(raw))); err != nil {
		return nil, notes, err
	}
	return out, notes, nil
}

// CheckDatabase verifies staged data and revokes old Runtime sessions and tokens.
// Sandbox identity, operation receipts and quota ledgers remain unchanged.
func CheckDatabase(ctx context.Context, stage string, m *Manifest) error {
	name := "runtime.db"
	if m.Mode == "sandbox" {
		name = "sandbox.db"
	}
	p := filepath.Join(stage, "data", name)
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("regular database required")
	}
	db, err := sqlite.Open(p)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.Check(ctx); err != nil {
		return err
	}
	schema, err := db.Schema(ctx)
	if err != nil {
		return err
	}
	if schema != m.Schema {
		return fmt.Errorf("database schema differs from backup manifest")
	}
	if m.Mode == "server" {
		key, err := os.Lstat(filepath.Join(stage, "data", "keys", "master.key"))
		if err != nil {
			return err
		}
		if !key.Mode().IsRegular() {
			return fmt.Errorf("master key missing")
		}
		return db.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			for _, bucket := range []string{"auth.sessions", "auth.tokens", "auth.login-links", "auth.challenges", "auth.totp_used"} {
				if _, err := tx.DeleteMatching(ctx, bucket, persistence.Query{Unordered: true}); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return nil
}

// RelocateLinks adjusts absolute links into managed storage while leaving
// relative links and references to external host directories unchanged.
func RelocateLinks(stage string, m *Manifest, dataDir, workRoot string) error {
	root, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer root.Close()
	sourceData := m.DataDir
	sourceWork := m.WorkRoot
	normalize := func(s string) string {
		if m.Build.OS == "windows" {
			return strings.ReplaceAll(s, "\\", "/")
		}
		return s
	}
	sourceData = normalize(sourceData)
	sourceWork = normalize(sourceWork)
	if sourceWork == "" {
		sourceWork = strings.TrimRight(sourceData, "/") + "/sandbox/work"
	}
	if workRoot == "" {
		workRoot = filepath.Join(dataDir, "sandbox", "work")
	}
	mappings := [][2]string{{sourceWork, workRoot}, {sourceData, dataDir}}
	for _, entry := range m.Files {
		if entry.Kind != "symlink" {
			continue
		}
		original := normalize(entry.Link)
		for _, mapping := range mappings {
			source := strings.TrimRight(mapping[0], "/")
			if source == "" {
				continue
			}
			name, base := original, source
			if m.Build.OS == "windows" {
				name, base = strings.ToLower(name), strings.ToLower(base)
			}
			if name != base && !strings.HasPrefix(name, base+"/") {
				continue
			}
			relative := strings.TrimPrefix(original[len(source):], "/")
			if relative != "" && !filepath.IsLocal(filepath.FromSlash(relative)) {
				return fmt.Errorf("managed link has an unsafe target: %s", entry.Path)
			}
			if !safeName(entry.Path) {
				return fmt.Errorf("invalid link path")
			}
			link := filepath.FromSlash(entry.Path)
			if err := root.Remove(link); err != nil {
				return err
			}
			if err := root.Symlink(filepath.Join(mapping[1], filepath.FromSlash(relative)), link); err != nil {
				return err
			}
			break
		}
	}
	return nil
}
