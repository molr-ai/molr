package app

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/textfiles"
)

type storagePath struct {
	path      string
	directory bool
}

// Only explicit configuration or an installation receipt matching this running
// instance may add roots. Never infer a directory from a development executable.
func storageSystemPaths(cfg config.Config, executable string) []storagePath {
	paths := []storagePath{{cfg.DataDir, true}, {executable, false}, {cfg.ConfigPath, false}, {cfg.Pricing.RulesFile, false}}
	for _, dir := range []string{cfg.Browser.AgentDirectory, cfg.Browser.ChromiumDirectory, cfg.Sandbox.RootFS} {
		if dir != "" {
			paths = append(paths, storagePath{dir, true})
		}
	}
	program := filepath.Dir(filepath.Dir(executable))
	record, err := installrecord.Read(filepath.Join(program, "installation.json"))
	if err == nil && record.State == "installed" && record.Mode == "server" && record.Paths.Program == program && filepath.Join(record.Paths.Config, "server.json") == cfg.ConfigPath && record.DataDir == cfg.DataDir {
		for _, dir := range []string{record.Paths.Program, record.Paths.Config, record.Paths.Cache, record.Paths.Data} {
			paths = append(paths, storagePath{dir, true})
		}
		paths = append(paths, storagePath{record.Paths.Unit, false})
	}
	// Bundled browser assets have a dedicated location, even without a receipt.
	if filepath.Base(executable) == "molr" || filepath.Base(executable) == "molr.exe" {
		paths = append(paths, storagePath{filepath.Join(filepath.Dir(executable), "libexec", "browser"), true})
	}
	return paths
}

func storageWithin(root, path string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
func sharedStoragePath(dataDir, path string) bool {
	rel, err := filepath.Rel(filepath.Join(dataDir, "users"), path)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) >= 2 && parts[0] != ".." && parts[1] == "global"
}

func systemStorage(ctx context.Context, cfg config.Config, database string, readyFiles map[string]bool, paths []storagePath) accountview.StorageSize {
	out := accountview.StorageSize{Available: true}
	work := cfg.Sandbox.WorkRoot
	if work == "" {
		work = filepath.Join(cfg.DataDir, "sandbox", "work")
	}
	measured := []storagePath{}
	excluded := func(path string) bool {
		if storageWithin(work, path) || sharedStoragePath(cfg.DataDir, path) || readyFiles[path] || path == database || path == database+"-wal" || path == database+"-shm" {
			return true
		}
		for _, previous := range measured {
			if previous.path == path || previous.directory && storageWithin(previous.path, path) {
				return true
			}
		}
		return false
	}
	for _, entry := range paths {
		if entry.path == "" {
			continue
		}
		if !filepath.IsAbs(entry.path) || filepath.Clean(entry.path) != entry.path || filepath.Dir(entry.path) == entry.path {
			return accountview.StorageSize{}
		}
		if excluded(entry.path) {
			continue
		}
		var size accountview.StorageSize
		if entry.directory {
			size = directoryStorageExcept(ctx, entry.path, excluded)
		} else {
			info, err := os.Lstat(entry.path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !info.Mode().IsRegular() {
				return accountview.StorageSize{}
			}
			size = accountview.StorageSize{Bytes: info.Size(), Files: 1, Available: true}
		}
		if !size.Available || size.Bytes < 0 || size.Bytes > math.MaxInt64-out.Bytes || size.Files > math.MaxInt64-out.Files {
			return accountview.StorageSize{}
		}
		out.Bytes += size.Bytes
		out.Files += size.Files
		measured = append(measured, entry)
	}
	return out
}

// Include every retained user-global directory, including accounts no longer in
// the active member list. Other files under users/ belong to system storage.
func sharedStorage(ctx context.Context, dataDir string) accountview.StorageSize {
	out := accountview.StorageSize{Available: true}
	root := filepath.Join(dataDir, "users")
	directory, err := textfiles.OpenDirectory(root, ".")
	if persistence.Missing(err) {
		return out
	}
	if err != nil {
		return accountview.StorageSize{}
	}
	defer directory.Close()
	count := 0
	for {
		if ctx.Err() != nil {
			return accountview.StorageSize{}
		}
		entries, err := directory.Readdir(256)
		if err != nil && !errors.Is(err, io.EOF) {
			return accountview.StorageSize{}
		}
		for _, info := range entries {
			count++
			if count > 1000000 {
				return accountview.StorageSize{}
			}
			if !info.IsDir() {
				continue
			}
			size := directoryStorage(ctx, filepath.Join(root, info.Name(), "global"))
			if !size.Available || size.Bytes > math.MaxInt64-out.Bytes || size.Files > math.MaxInt64-out.Files {
				return accountview.StorageSize{}
			}
			out.Bytes += size.Bytes
			out.Files += size.Files
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return out
}
