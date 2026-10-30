package api

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"strings"
)

func explicitGrantExpiry(raw []byte, value any) error {
	required := false
	switch q := value.(type) {
	case policy.GrantRequest:
		required = q.Scope.FullAccess
	case policy.UpdateGrantRequest:
		required = q.Scope.FullAccess
	case policy.PermissionResolution:
		if q.Decision == "approve" {
			required = q.Scope.FullAccess
			for _, item := range q.Additional {
				required = required || item.Scope.FullAccess
			}
		}
	}
	if !required {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for key := range fields {
		if strings.EqualFold(key, "ExpiresAt") {
			return nil
		}
	}
	return core.Invalid("expires_at", "explicit expiry required; use null only for deliberately permanent access")
}
