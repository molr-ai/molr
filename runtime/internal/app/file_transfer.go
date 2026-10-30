package app

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
)

const transferLimit = 20 << 20

func (f *resourceFileTools) transfer(ctx context.Context, e core.Execution, r tools.FileTransferRequest) (json.RawMessage, error) {
	// Resolve and authorize both ends before reading or creating anything.
	for i, t := range []tools.FileTarget{r.Source, r.Destination} {
		action := policy.Read
		if i == 1 {
			action = policy.Write
		}
		id, err := f.router.location(ctx, e, t)
		if err != nil {
			return nil, err
		}
		if id == "local" {
			if _, _, err = f.resolve(ctx, e, t, action); err != nil {
				return nil, err
			}
		} else {
			if _, err = f.router.scope(ctx, e, id, t, action); err != nil {
				return nil, err
			}
		}
	}
	admitted := map[core.ID]bool{}
	if err := f.adapters.store.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		for _, target := range []tools.FileTarget{r.Source, r.Destination} {
			id := targetSandbox(target.SandboxID)
			if target.FullAccess && !admitted[id] {
				if err := f.router.consumeFullAccess(ctx, e, id); err != nil {
					return err
				}
				admitted[id] = true
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, fullAccessAdmissionsKey{}, admitted)
	var blob sandboxBlob
	if targetSandbox(r.Source.SandboxID) != "local" {
		data, _, err := f.router.file(ctx, e, "download", r.Source, sandboxDownload{Target: r.Source, MaxBytes: transferLimit}, "")
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &blob); err != nil {
			return nil, err
		}
	} else {
		root, name, err := f.resolve(ctx, e, r.Source, policy.Read)
		if err != nil {
			return nil, err
		}
		file, err := openResourceFile(root, name)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		blob.Version, err = resourceFileVersion(file)
		if err != nil {
			return nil, err
		}
		blob.Data, err = io.ReadAll(io.LimitReader(file, transferLimit+1))
		if err != nil {
			return nil, err
		}
		after, err := resourceFileVersion(file)
		if err != nil {
			return nil, err
		}
		if after != blob.Version {
			return nil, fileChanged()
		}
	}
	if len(blob.Data) > transferLimit {
		return nil, core.Invalid("file", "transfer exceeds 20 MiB")
	}
	if r.SourceVersion != "" && r.SourceVersion != blob.Version {
		return nil, fileChanged()
	}
	var version string
	if targetSandbox(r.Destination.SandboxID) != "local" {
		data, _, err := f.router.file(ctx, e, "upload", r.Destination, sandboxUpload{Target: r.Destination, Data: blob.Data, Overwrite: r.Overwrite, ExpectedVersion: r.ExpectedVersion}, "")
		if err != nil {
			return nil, err
		}
		var result sandboxBlob
		if err = json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		version = result.Version
	} else {
		root, name, err := f.resolve(ctx, e, r.Destination, policy.Write)
		if err != nil {
			return nil, err
		}
		unlock, err := f.Lock(ctx, filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		defer unlock()
		if err = f.checkWorkspaceFileSize(ctx, e, r.Destination, int64(len(blob.Data))); err != nil {
			return nil, err
		}
		version, err = replaceResourceFile(ctx, root, name, string(blob.Data), r.Overwrite, r.ExpectedVersion, func() error { _, _, err := f.resolve(ctx, e, r.Destination, policy.Write); return err })
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(struct {
		Source, Destination            tools.FileTarget
		SourceVersion, Version, SHA256 string
		Bytes                          int
	}{r.Source, r.Destination, blob.Version, version, digestBytes(blob.Data), len(blob.Data)})
}
