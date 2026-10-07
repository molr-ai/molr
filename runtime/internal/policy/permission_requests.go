package policy

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
)

// PermissionRequestInput describes a need, never an authorization or an owner identity.
type PermissionRequestInput struct {
	FullAccess   bool                     `json:"full_access,omitempty"`
	Additional   []PermissionRequestInput `json:"additional_requests,omitempty"`
	Constraints  *Constraints             `json:"constraints,omitempty"`
	Temporary    *TemporaryConstraints    `json:"temporary,omitempty"`
	GrantID      core.ID                  `json:"grant_id,omitempty"`
	GrantVersion core.Version             `json:"grant_version,omitempty"`

	ResourceKind        string     `json:"resource_kind"`
	ResourceID          core.ID    `json:"resource_id,omitempty"`
	ModelID             core.ID    `json:"model_id,omitempty"`
	ResourceDescription string     `json:"resource_description"`
	Actions             []Action   `json:"actions"`
	PathPrefixes        []string   `json:"path_prefixes,omitempty"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	Reason              string     `json:"reason"`
}

func (r *PermissionRequestInput) Normalize(now time.Time) error {
	if r.FullAccess && (r.ResourceKind != "compute" || r.Constraints != nil || r.Temporary != nil) {
		return core.Invalid("full_access", "Full Access requires compute permission without quotas")
	}
	if len(r.Additional) > 7 {
		return core.Invalid("additional_requests", "at most eight permissions in one request")
	}
	for i := range r.Additional {
		if r.Additional[i].ResourceKind != "compute" && r.Additional[i].ResourceKind != "network" && r.Additional[i].ResourceKind != "storage" {
			return core.Invalid(fmt.Sprintf("additional_requests[%d].resource_kind", i), "grouped requests support compute, network and workspace resources")
		}
		if len(r.Additional[i].Additional) > 0 {
			return core.Invalid("additional_requests", "nested groups are not supported")
		}
		// The root deadline is the authorization time for the whole group.
		r.Additional[i].ExpiresAt = r.ExpiresAt
		if err := r.Additional[i].Normalize(now); err != nil {
			return PermissionRequestItemError(i+1, err)
		}
	}

	if !slices.Contains([]string{"storage", "compute", "network", "model", "tool"}, r.ResourceKind) {
		return core.Invalid("resource_kind", "unsupported resource kind; expected storage, compute, network, model or tool")
	}
	if r.ResourceID.Validate() != nil {
		return core.Invalid("resource_id", "select an existing resource ID from permission_list Requestable")
	}
	if err := r.Constraints.Validate(); err != nil {
		return err
	}
	if r.Constraints != nil && r.Constraints.Kind() != r.ResourceKind {
		return core.Invalid("constraints", "resource kind mismatch")
	}
	if (r.GrantID != "" || r.GrantVersion != 0) && (r.GrantID.Validate() != nil || r.GrantVersion < 1) {
		return core.Invalid("grant_id", "existing grant and version required")
	}
	if r.Temporary != nil {
		if r.Constraints == nil || r.Temporary.Limits.Kind() != r.ResourceKind || !r.Temporary.Until.After(now) || (r.ExpiresAt != nil && r.Temporary.Until.After(*r.ExpiresAt)) {
			return core.Invalid("temporary", "matching limits and future expiry within authorization expiration required")
		}
		if err := r.Temporary.Limits.Validate(); err != nil {
			return err
		}
	}
	r.Reason = strings.TrimSpace(r.Reason)
	r.ResourceDescription = strings.TrimSpace(r.ResourceDescription)
	if len(r.Actions) == 0 || len(r.Actions) > 4 || r.Reason == "" || len(r.Reason) > 2000 || r.ResourceDescription == "" || len(r.ResourceDescription) > 1000 {
		return core.Invalid("request", "resource kind, description, actions and reason required")
	}
	for _, id := range []core.ID{r.ResourceID, r.ModelID} {
		if id != "" && id.Validate() != nil {
			return core.Invalid("id", "invalid resource or model ID")
		}
	}
	for _, action := range r.Actions {
		if action != Read && action != Write && action != Execute {
			return core.Invalid("actions", "unsupported action")
		}
		if (r.ResourceKind == "compute" || r.ResourceKind == "network") && action != Execute || r.ResourceKind == "storage" && action == Execute || r.ResourceKind == "model" && action == Write {
			return core.Invalid("actions", "action does not match resource kind")
		}
	}
	if r.ResourceKind == "model" && r.ResourceID != "" && r.ModelID == "" {
		return core.Invalid("model_id", "select an exact requestable model")
	}
	if r.ResourceKind != "model" && r.ModelID != "" || r.ModelID != "" && r.ResourceID == "" {
		return core.Invalid("model_id", "model requires a model resource")
	}
	if r.ResourceKind == "storage" && len(r.PathPrefixes) == 0 {
		return core.Invalid("path_prefixes", "explicit storage scope required; / means the whole resource")
	}
	if r.ResourceKind != "storage" && len(r.PathPrefixes) > 0 || len(r.PathPrefixes) > 32 {
		return core.Invalid("path_prefixes", "storage paths only, at most 32")
	}
	for _, prefix := range r.PathPrefixes {
		if !strings.HasPrefix(prefix, "/") || path.Clean(prefix) != prefix || strings.ContainsRune(prefix, 0) || len(prefix) > 4096 {
			return core.Invalid("path_prefixes", "clean resource-relative absolute paths required")
		}
	}
	if r.ExpiresAt == nil || !r.ExpiresAt.After(now) {
		return core.Invalid("expires_at", "explicit future authorization expiration required")
	}
	raw, _ := json.Marshal(r)
	if len(raw) > 4096 {
		return core.Invalid("request", "permission request must fit within 4096 bytes")
	}
	slices.Sort(r.Actions)
	r.Actions = slices.Compact(r.Actions)
	slices.Sort(r.PathPrefixes)
	r.PathPrefixes = slices.Compact(r.PathPrefixes)
	return nil
}

type PermissionRequest struct {
	GrantedAll                       []Grant `json:",omitempty"`
	ID, OwnerID, AssistantID, TaskID core.ID
	Input                            PermissionRequestInput
	State                            string
	GrantID                          core.ID
	MessageID                        core.ID
	Granted                          *Grant `json:",omitempty"`
	CreatedAt                        time.Time
	ResolvedAt                       *time.Time
	Version                          core.Version
}

type PermissionResolution struct {
	Additional   []PermissionResolution `json:",omitempty"`
	Constraints  *Constraints           `json:",omitempty"`
	Temporary    *TemporaryConstraints  `json:",omitempty"`
	GrantID      core.ID
	GrantVersion core.Version

	Mutation   core.Mutation
	RequestID  core.ID
	Decision   string
	ResourceID core.ID
	Actions    []Action
	Scope      Scope
	ExpiresAt  *time.Time
}

// PermissionRequestItemError locates a safe validation diagnostic in a grouped
// proposal. Copy the error without wrapping so tool results retain the full field.
func PermissionRequestItemError(index int, err error) error {
	if index == 0 {
		return err
	}
	if detail, ok := err.(*core.Error); ok {
		out := *detail
		out.Field = fmt.Sprintf("additional_requests[%d]", index-1)
		if detail.Field != "" {
			out.Field += "." + detail.Field
		}
		return &out
	}
	return err
}
