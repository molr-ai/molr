//go:build !linux

package textfiles

import (
	"context"
	"molr.ai/runtime/internal/core"
	"os"
)

func resourceFilesUnsupported() error {
	return core.NewError(core.FailedPrecondition, "local file tools require Linux no-symlink confinement")
}
func Open(string, string) (*os.File, error)          { return nil, resourceFilesUnsupported() }
func OpenDirectory(string, string) (*os.File, error) { return nil, resourceFilesUnsupported() }
func Version(*os.File) (string, error)               { return "", resourceFilesUnsupported() }
func Replace(context.Context, string, string, string, bool, string, func() error) (string, error) {
	return "", resourceFilesUnsupported()
}
func Changed() error {
	return core.WithReason(core.NewError(core.Conflict, "file changed; read it again before editing"), "file_version_conflict")
}
func CreateOutput(string, string) (*os.File, error) { return nil, resourceFilesUnsupported() }

func AppendOutput(directory, name string, offset int64, data []byte) error {
	return core.NewError(core.Unavailable, "confined file writes unavailable")
}

func MkdirAll(string, string) error { return resourceFilesUnsupported() }
func Remove(context.Context, string, string, string, func() error) error {
	return resourceFilesUnsupported()
}

func Move(ctx context.Context, root, from, to, expected string, directory bool, authorize func() error) (string, error) {
	return "", core.NewError(core.Unavailable, "safe filesystem mutations require Linux")
}
func RemoveDirectory(ctx context.Context, root, name, expected string, authorize func() error) error {
	return core.NewError(core.Unavailable, "safe filesystem mutations require Linux")
}
