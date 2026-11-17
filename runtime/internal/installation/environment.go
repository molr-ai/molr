//go:build linux || darwin

package installation

import (
	"os"

	"molr.ai/runtime/internal/execenv"
	"molr.ai/runtime/internal/installrecord"
)

func (e *Engine) sourceEnvironment() []string {
	if e.environment != nil {
		return e.environment
	}
	return os.Environ()
}

func (e *Engine) writeEnvironment(r *installrecord.Record, selected map[string]string) error {
	if selected == nil {
		var err error
		selected, err = execenv.Capture(e.Options.Environment, e.sourceEnvironment())
		if err != nil {
			return err
		}
	}
	return execenv.Save(e.Ctx, r.DataDir, selected)
}
