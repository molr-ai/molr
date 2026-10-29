package resource

import "molr.ai/runtime/internal/core"

type LocalProvider struct {
	Name       string
	Preset     string
	Endpoint   string
	State      string
	Models     []string
	ResourceID core.ID
}
