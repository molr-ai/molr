package app

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/quota"
	"molr.ai/runtime/internal/textfiles"
)

func storageUsage(ctx context.Context, store persistence.Store, identity *auth.Manager, cfg config.Config, p core.Principal, admin bool) (accountview.StorageUsage, error) {
	out := accountview.StorageUsage{AsOf: time.Now().UTC(), DatabasePhysical: admin}
	if _, err := identity.CurrentUser(ctx, p); err != nil {
		return out, err
	}
	if admin {
		if err := identity.RequireAdministrator(ctx, p); err != nil {
			return out, err
		}
	}
	owner := p.OwnerID
	if admin {
		owner = ""
	}
	var assistants []struct{ ID core.ID }
	readyFiles := map[string]bool{}
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if !admin {
			if reader, ok := store.(interface {
				OwnerRecordBytes(context.Context, core.ID) (int64, error)
			}); ok {
				owned, err := reader.OwnerRecordBytes(ctx, owner)
				if err != nil {
					return err
				}
				shared, err := contextstore.AccountUsage(ctx, tx, owner)
				if err != nil {
					return err
				}
				if owned > math.MaxInt64-shared.Bytes {
					return core.NewError(core.Internal, "storage usage overflow")
				}
				out.Database = accountview.StorageSize{Bytes: owned + shared.Bytes, Available: true}
			}
			var err error
			assistants, err = persistence.Select[struct{ ID core.ID }](ctx, tx.Store(), "business.assistants", persistence.Query{Unordered: true, Fields: []string{"ID"}, Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}})
			if err != nil {
				return err
			}
		}
		bytes, err := quota.AttachmentBytes(ctx, tx.Store(), owner)
		if err != nil {
			return err
		}
		q := persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "ready")}}
		if owner != "" {
			q.Where = append(q.Where, persistence.Eq("OwnerID", owner))
		}
		count, err := tx.Count(ctx, "storage.files", q)
		if err != nil {
			return err
		}
		out.Attachments = accountview.StorageSize{Bytes: bytes, Files: count, Available: true}
		if admin {
			q.Fields = []string{"ID"}
			files, err := persistence.Select[struct{ ID core.ID }](ctx, tx.Store(), "storage.files", q)
			if err != nil {
				return err
			}
			for _, file := range files {
				if file.ID.Validate() != nil {
					return core.NewError(core.Internal, "invalid stored file identity")
				}
				readyFiles[filepath.Join(cfg.DataDir, "files", "objects", string(file.ID))] = true
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	// Filesystem reads happen after the database transaction, not while holding its lock.
	if admin {
		if database, ok := store.(interface{ Path() string }); ok {
			out.Database = databaseFileUsage(database.Path())
		}
		root := cfg.Sandbox.WorkRoot
		if root == "" {
			root = filepath.Join(cfg.DataDir, "sandbox", "work")
		}
		out.Workspaces = directoryStorage(ctx, root)
		out.Shared = sharedStorage(ctx, cfg.DataDir)
		databasePath := filepath.Join(cfg.DataDir, "runtime.db")
		if database, ok := store.(interface{ Path() string }); ok {
			databasePath = database.Path()
		}
		executable, err := os.Executable()
		if err != nil {
			return out, err
		}
		system := systemStorage(ctx, cfg, databasePath, readyFiles, storageSystemPaths(cfg, executable))
		out.System = &system
	} else {
		out.Shared = directoryStorage(ctx, platform.UserGlobal(cfg.DataDir, owner))
		out.Workspaces.Available = true
		for _, assistant := range assistants {
			if err := assistant.ID.Validate(); err != nil {
				return out, err
			}
			measured := directoryStorage(ctx, platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, owner, assistant.ID))
			if !measured.Available || measured.Bytes > math.MaxInt64-out.Workspaces.Bytes || measured.Files > math.MaxInt64-out.Workspaces.Files {
				out.Workspaces = accountview.StorageSize{}
				break
			}
			out.Workspaces.Bytes += measured.Bytes
			out.Workspaces.Files += measured.Files
		}
	}
	if err := persistence.CheckContext(ctx); err != nil {
		return accountview.StorageUsage{}, err
	}
	return out, nil
}

func databaseFileUsage(path string) accountview.StorageSize {
	out := accountview.StorageSize{Available: true}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > math.MaxInt64-out.Bytes {
			return accountview.StorageSize{}
		}
		out.Bytes += info.Size()
		out.Files++
	}
	return out
}

// Confined metadata-only traversal: never follow links or open file contents.
// The request context bounds runtime; a large/incomplete scan is unavailable,
// not a misleading partial total. The HTTP snapshot is cached for five minutes.
func directoryStorage(ctx context.Context, root string) accountview.StorageSize {
	return directoryStorageExcept(ctx, root, nil)
}

func directoryStorageExcept(ctx context.Context, root string, excluded func(string) bool) accountview.StorageSize {
	out := accountview.StorageSize{Available: true}
	pending := []string{"."}
	visited := 0
	for len(pending) > 0 {
		if ctx.Err() != nil {
			return accountview.StorageSize{}
		}
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		directory, err := textfiles.OpenDirectory(root, name)
		if persistence.Missing(err) {
			continue
		}
		if err != nil {
			return accountview.StorageSize{}
		}
		complete := true
		for {
			if ctx.Err() != nil {
				complete = false
				break
			}
			entries, err := directory.Readdir(256)
			if err != nil && !errors.Is(err, io.EOF) {
				complete = false
				break
			}
			for _, info := range entries {
				visited++
				if visited > 1000000 {
					complete = false
					break
				}
				if excluded != nil && excluded(filepath.Join(root, name, info.Name())) {
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 {
					continue
				}
				if info.IsDir() {
					pending = append(pending, filepath.Join(name, info.Name()))
					continue
				}
				if !info.Mode().IsRegular() {
					continue
				}
				if info.Size() < 0 || info.Size() > math.MaxInt64-out.Bytes {
					complete = false
					break
				}
				out.Bytes += info.Size()
				out.Files++
			}
			if !complete || errors.Is(err, io.EOF) {
				break
			}
		}
		directory.Close()
		if !complete {
			return accountview.StorageSize{}
		}
	}
	return out
}
