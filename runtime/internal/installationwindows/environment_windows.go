package installationwindows

import (
	"context"
	"os"

	"molr.ai/runtime/internal/execenv"
	"molr.ai/runtime/internal/platform"
)

// Installation creates the private directory before calling the shared writer.
func writeEnvironment(ctx context.Context, directory string, selected map[string]string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	if err := platform.PrivateDirectory(directory); err != nil {
		return err
	}
	return execenv.Save(ctx, directory, selected)
}
