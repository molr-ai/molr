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
type HiringPermissionProposal struct {
	MaxAssistants int        `json:"max_assistants"`
	Budget        core.Money `json:"budget"`
	GrantIDs      []core.ID  `json:"grant_ids"`
}
type CommunicationPermissionProposal struct {
	AllAssistants bool      `json:"all_assistants"`
	AssistantIDs  []core.ID `json:"assistant_ids"`
}

type PermissionRequestInput struct {
	PermissionType  string                           `json:"permission_type,omitempty"`
	Hiring          *HiringPermissionProposal        `json:"hiring,omitempty"`
	Communication   *CommunicationPermissionProposal `json:"communication,omitempty"`
	Once            bool                             `json:"once,omitempty"`
	FullAccessScope *FullAccessScope                 `json:"full_access_scope,omitempty"`
	FullAccess      bool                             `json:"full_access,omitempty"`
	Additional      []PermissionRequestInput         `json:"additional_requests,omitempty"`
	Constraints     *Constraints                     `json:"constraints,omitempty"`
	Temporary       *TemporaryConstraints            `json:"temporary,omitempty"`
	GrantID         core.ID                          `json:"grant_id,omitempty"`
	GrantVersion    core.Version                     `json:"grant_version,omitempty"`

	ResourceKind        string     `json:"resource_kind,omitempty"`
	ResourceID          core.ID    `json:"resource_id,omitempty"`
	ModelID             core.ID    `json:"model_id,omitempty"`
	ResourceDescription string     `json:"resource_description,omitempty"`
	Actions             []Action   `json:"actions,omitempty"`
	PathPrefixes        []string   `json:"path_prefixes,omitempty"`
	ExpiresAt           *time.Time `json:"expires_at"`
	Reason              string     `json:"reason"`
}

func (r *PermissionRequestInput) Normalize(now time.Time) error {
	if r.PermissionType == "resource" {
		r.PermissionType = ""
	}
	if r.PermissionType != "" {
		return r.normalizeDelegation(now)
	}
	if r.Hiring != nil || r.Communication != nil {
		return core.Invalid("permission_type", "delegation payload requires its permission type")
	}
	if r.Once && !r.FullAccess {
		return core.Invalid("once", "one-use authorization requires Full Access")
	}
	if r.FullAccess {
		if err := r.FullAccessScope.Validate(); err != nil {
			return err
		}
	} else if r.FullAccessScope != nil {
		return core.Invalid("full_access_scope", "scope requires Full Access")
	}
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
	ApprovedAssistantVersion         core.Version            `json:",omitempty"`
	ApprovedDelegation               *PermissionRequestInput `json:",omitempty"`
	GrantedAll                       []Grant                 `json:",omitempty"`
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
	Hiring           *HiringPermissionProposal        `json:",omitempty"`
	Communication    *CommunicationPermissionProposal `json:",omitempty"`
	AssistantVersion core.Version                     `json:",omitempty"`
	Additional       []PermissionResolution           `json:",omitempty"`
	Constraints      *Constraints                     `json:",omitempty"`
	Temporary        *TemporaryConstraints            `json:",omitempty"`
	GrantID          core.ID
	GrantVersion     core.Version

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

func (r *PermissionRequestInput) normalizeDelegation(now time.Time) error {
	if r.PermissionType != "temporary_hiring" && r.PermissionType != "assistant_communication" {
		return core.Invalid("permission_type", "unknown permission type")
	}
	if r.ResourceKind != "" || r.ResourceID != "" || r.ModelID != "" || len(r.Actions) > 0 || len(r.PathPrefixes) > 0 || r.ResourceDescription != "" || r.FullAccess || r.Once || r.FullAccessScope != nil || r.Constraints != nil || r.Temporary != nil || r.GrantID != "" || r.GrantVersion != 0 || len(r.Additional) > 0 {
		return core.Invalid("request", "one delegation permission per request; resource fields are not applicable")
	}
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Reason == "" || len(r.Reason) > 2000 || r.ExpiresAt != nil && !r.ExpiresAt.After(now) {
		return core.Invalid("request", "reason and a future validity time or null required")
	}
	var ids []core.ID
	if r.PermissionType == "temporary_hiring" {
		if r.Hiring == nil || r.Communication != nil {
			return core.Invalid("hiring", "hiring proposal required")
		}
		h := r.Hiring
		if h.MaxAssistants < 1 || h.MaxAssistants > 32 || h.Budget.Validate() != nil || h.Budget.Micros <= 0 || len(h.GrantIDs) > 64 {
			return core.Invalid("hiring", "valid active-worker limit, shared total budget and grants required")
		}
		slices.Sort(h.GrantIDs)
		h.GrantIDs = slices.Compact(h.GrantIDs)
		ids = h.GrantIDs
	} else {
		if r.Communication == nil || r.Hiring != nil {
			return core.Invalid("communication", "communication proposal required")
		}
		c := r.Communication
		if len(c.AssistantIDs) > 200 || c.AllAssistants && len(c.AssistantIDs) > 0 {
			return core.Invalid("assistant_ids", "choose all assistants or specified IDs")
		}
		slices.Sort(c.AssistantIDs)
		c.AssistantIDs = slices.Compact(c.AssistantIDs)
		ids = c.AssistantIDs
	}
	for _, id := range ids {
		if id.Validate() != nil {
			return core.Invalid("id", "invalid grant or assistant ID")
		}
	}
	raw, _ := json.Marshal(r)
	if len(raw) > 16384 {
		return core.Invalid("request", "delegation request too large")
	}
	return nil
}
