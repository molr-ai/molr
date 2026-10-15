package app

import (
	"molr.ai/runtime/internal/installation"
	"molr.ai/runtime/internal/maintenance"
)

func newMaintenance(config string, external bool) maintenance.Service {
	return installation.NewMaintenance(config, external)
}
