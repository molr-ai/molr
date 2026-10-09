package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
	"path"
)

// A turn may narrow file access and observe mutations. Parsing, execution,
// permissions, receipts and result formats remain in the common file tools.
type filePolicy struct {
	Check  func(context.Context, core.Execution, tools.FileTarget, ...policy.Action) error
	Read   func(context.Context, core.Execution, tools.FileTarget) error
	Before func(context.Context, core.Execution, string, tools.FileTarget, *tools.FileTarget, string, []byte) (func() error, error)
}
type filePolicyKey struct{}

func checkFilePolicy(ctx context.Context, e core.Execution, t tools.FileTarget, actions ...policy.Action) error {
	p, _ := ctx.Value(filePolicyKey{}).(*filePolicy)
	if p != nil && p.Check != nil {
		return p.Check(ctx, e, t, actions...)
	}
	return nil
}
func observeFileRead(ctx context.Context, e core.Execution, t tools.FileTarget) error {
	p, _ := ctx.Value(filePolicyKey{}).(*filePolicy)
	if p != nil && p.Read != nil {
		return p.Read(ctx, e, t)
	}
	return nil
}
func prepareFileMutation(ctx context.Context, e core.Execution, kind string, t tools.FileTarget, to *tools.FileTarget, version string, after []byte) (func() error, error) {
	p, _ := ctx.Value(filePolicyKey{}).(*filePolicy)
	if p != nil && p.Before != nil {
		return p.Before(ctx, e, kind, t, to, version, after)
	}
	return func() error { return nil }, nil
}
func (f *resourceFileTools) makeParents(ctx context.Context, e core.Execution, t tools.FileTarget, root, name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	if dir, err := textfiles.OpenDirectory(root, parent); err == nil {
		dir.Close()
		return nil
	} else if core.CodeOf(err) != core.NotFound {
		return err
	}
	target := t
	target.Path = path.Dir(t.Path)
	resolved, resolvedName, err := f.resolve(ctx, e, target, policy.Write)
	if err != nil {
		return err
	}
	if resolved != root || resolvedName != parent {
		return fileChanged()
	}
	if err = f.makeParents(ctx, e, target, root, parent); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return textfiles.MkdirAll(root, parent)
}

func (f *resourceFileTools) move(ctx context.Context, e core.Execution, r tools.FileMoveRequest) (json.RawMessage, error) {
	if r.ExpectedVersion == "" || r.ToPath == "" || r.Path == r.ToPath {
		return nil, core.Invalid("move", "distinct destination and current file version required")
	}
	if f.router != nil {
		if out, remote, err := f.router.file(ctx, e, "file_move", r.FileTarget, r, ""); remote {
			return out, err
		}
	}
	root, from, err := f.resolve(ctx, e, r.FileTarget, policy.Write)
	if err != nil {
		return nil, err
	}
	dest := r.FileTarget
	dest.Path = r.ToPath
	destRoot, to, err := f.resolve(ctx, e, dest, policy.Write)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, core.Invalid("to_path", "distinct file required")
	}
	if root != destRoot {
		return nil, core.Invalid("to_path", "move must stay in one storage root")
	}
	first, second := from, to
	if first > second {
		first, second = second, first
	}
	unlock, err := f.lock(ctx, root+"/"+first)
	if err != nil {
		return nil, err
	}
	defer unlock()
	unlockSecond, err := f.lock(ctx, root+"/"+second)
	if err != nil {
		return nil, err
	}
	defer unlockSecond()
	check := func() error {
		a, x, err := f.resolve(ctx, e, r.FileTarget, policy.Write)
		if err != nil {
			return err
		}
		b, y, err := f.resolve(ctx, e, dest, policy.Write)
		if err != nil {
			return err
		}
		if a != root || b != root || x != from || y != to {
			return fileChanged()
		}
		return nil
	}
	source, err := openResourceFile(root, from)
	if err != nil {
		return nil, err
	}
	actual, err := resourceFileVersion(source)
	source.Close()
	if err != nil {
		return nil, err
	}
	if actual != r.ExpectedVersion {
		return nil, fileChanged()
	}
	if r.CreateParents {
		if err = f.makeParents(ctx, e, dest, root, to); err != nil {
			return nil, err
		}
	}
	done, err := prepareFileMutation(ctx, e, "file_move", r.FileTarget, &dest, r.ExpectedVersion, nil)
	if err != nil {
		return nil, err
	}
	version, err := textfiles.Move(ctx, root, from, to, r.ExpectedVersion, false, check)
	if err != nil {
		return nil, err
	}
	if err = done(); err != nil {
		return nil, core.Wrap(core.Unavailable, "file committed; history update failed", err)
	}
	return json.Marshal(struct{ Path, ToPath, Version string }{fileVirtualPath(r.FileTarget), fileVirtualPath(dest), version})
}
func (f *resourceFileTools) delete(ctx context.Context, e core.Execution, r tools.FileDeleteRequest) (json.RawMessage, error) {
	if r.ExpectedVersion == "" {
		return nil, core.Invalid("expected_version", "current file version required")
	}
	if f.router != nil {
		if out, remote, err := f.router.file(ctx, e, "file_delete", r.FileTarget, r, ""); remote {
			return out, err
		}
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Write)
	if err != nil {
		return nil, err
	}
	unlock, err := f.lock(ctx, root+"/"+name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	check := func() error {
		current, p, err := f.resolve(ctx, e, r.FileTarget, policy.Write)
		if err != nil {
			return err
		}
		if current != root || p != name {
			return fileChanged()
		}
		return nil
	}
	done, err := prepareFileMutation(ctx, e, "file_delete", r.FileTarget, nil, r.ExpectedVersion, nil)
	if err != nil {
		return nil, err
	}
	if err = textfiles.Remove(ctx, root, name, r.ExpectedVersion, check); err != nil {
		return nil, err
	}
	if err = done(); err != nil {
		return nil, core.Wrap(core.Unavailable, "file committed; history update failed", err)
	}
	return json.Marshal(struct {
		Path    string
		Deleted bool
	}{fileVirtualPath(r.FileTarget), true})
}
