package policy

import "molr.ai/runtime/internal/core"

type grantUseBinding struct {
	GrantID     core.ID
	AssistantID core.ID
	ExecutionID core.ID
}

func grantUseKey(owner, resource, operation core.ID) string {
	return string(owner) + "/" + string(resource) + "/" + string(operation)
}
