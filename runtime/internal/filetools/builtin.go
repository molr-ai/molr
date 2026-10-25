package filetools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/molrdocs"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
	"os"
	"strings"
)

const BuiltinRoot = "builtin:molr"

func IsBuiltin(t tools.FileTarget) bool {
	return t.ResourceID == "" && (t.Path == molrdocs.Root || strings.HasPrefix(t.Path, molrdocs.Root+"/"))
}

type readableFile interface {
	fs.File
	io.Seeker
}

func openTextSource(root, name string) (readableFile, error) {
	if root != BuiltinRoot {
		return textfiles.Open(root, name)
	}
	f, err := molrdocs.FS.Open(name)
	if err != nil {
		return nil, builtinFileError(err)
	}
	if r, ok := f.(readableFile); ok {
		return r, nil
	}
	f.Close()
	return nil, core.Invalid("path", "text file required")
}
func textSourceVersion(f fs.File) (string, error) {
	if file, ok := f.(*os.File); ok {
		return textfiles.Version(file)
	}
	return molrdocs.Revision, nil
}
func openDirectorySource(root, name string) (fs.ReadDirFile, error) {
	if root != BuiltinRoot {
		return textfiles.OpenDirectory(root, name)
	}
	f, err := molrdocs.FS.Open(name)
	if err != nil {
		return nil, builtinFileError(err)
	}
	if d, ok := f.(fs.ReadDirFile); ok {
		return d, nil
	}
	f.Close()
	return nil, core.Invalid("path", "directory required")
}
func builtinFileError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return core.NewError(core.NotFound, "built-in document not found")
	}
	if errors.Is(err, fs.ErrInvalid) {
		return core.Invalid("path", "invalid built-in document path")
	}
	return err
}
func (f *Tools) readBuiltin(ctx context.Context, e core.Execution, r tools.FileReadRequest) (json.RawMessage, error) {
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read)
	if err != nil {
		return nil, err
	}
	if r.Detail != "" || r.StartSeconds != nil || r.EndSeconds != nil || r.MaxFrames != 0 {
		return nil, core.Invalid("options", "media options cannot be used with documentation")
	}
	if !r.MetadataOnly {
		return f.readText(ctx, e, r)
	}
	file, err := openTextSource(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return json.Marshal(MediaReadResult{SandboxID: "local", Path: r.Path, Version: molrdocs.Revision, MediaType: "text/plain; charset=utf-8", SizeBytes: info.Size()})
}
