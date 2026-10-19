package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/modelcatalog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

type resourceOperation struct {
	Action    policy.Action
	Via       string
	Available bool
	Reason    string
}
type permissionGrant struct {
	Version     core.Version
	Constraints *policy.Constraints          `json:",omitempty"`
	Temporary   *policy.TemporaryConstraints `json:",omitempty"`
	ID          core.ID
	Actions     []policy.Action
	Scope       policy.Scope
	ExpiresAt   *time.Time
}

type resourceCapability struct {
	FullAccess       bool                    `json:",omitempty"`
	Usage            *quotaUsage             `json:",omitempty"`
	Network          *networkPermissionScope `json:",omitempty"`
	Grants           []permissionGrant
	SandboxID        core.ID `json:",omitempty"`
	ResourceID       core.ID
	Name             string
	Kind             resource.Kind
	Source           string
	Path             string  `json:",omitempty"`
	PathMeaning      string  `json:",omitempty"`
	FilePath         string  `json:",omitempty"`
	ShellPath        string  `json:",omitempty"`
	ShellEnvironment string  `json:",omitempty"`
	ModelID          core.ID `json:",omitempty"`
	ModelName        string  `json:",omitempty"`
	Operations       []resourceOperation
}
type builtinStorage struct {
	SandboxID       core.ID `json:",omitempty"`
	Kind            string
	Source          string
	Operations      []resourceOperation
	Path            string
	Lifetime        string
	Available       bool
	Reason          string
	Actions         []policy.Action
	ShellComputeIDs []core.ID
}
type sandboxDescriptor struct {
	FullAccessComputeIDs []core.ID `json:",omitempty"`
	Description          string    `json:",omitempty"`
	ID                   core.ID
	Name                 string
	Local, Storage       bool
	Capabilities         platform.Capabilities
}
type resourceListing struct {
	CurrentTime          time.Time
	Requestable          []requestablePermission
	RequestableTruncated bool
	Sandboxes            []sandboxDescriptor `json:",omitempty"`
	Resources            []resourceCapability
	BuiltInStorage       []builtinStorage
	Limit                int
	Truncated            bool
}
type resourceCatalog struct {
	adapters adapters
	shell    *shellService
}

// This is a current capability snapshot, not a second authorization system.
// Per-scope rows prevent a write grant on one directory implying writes elsewhere.
func (c resourceCatalog) list(ctx context.Context, p core.Principal) (json.RawMessage, error) {
	a := c.adapters
	if p.Kind != core.ActorAssistant {
		return nil, denied()
	}
	if err := a.policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: "permission_list"}, Action: policy.Execute}); err != nil {
		return nil, err
	}
	shellReason := "execution_disabled"
	if c.shell != nil {
		caps, err := c.shell.runner.Capabilities(ctx)
		if err != nil {
			return nil, err
		}
		shellReason = caps.Reason
		if caps.Shell {
			shellReason = ""
		}
	}
	out := resourceListing{CurrentTime: time.Now().UTC(), Resources: []resourceCapability{}, BuiltInStorage: []builtinStorage{}, Limit: 200}
	err := a.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		grants, err := persistence.Select[policy.Grant](ctx, a.store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("RevokedAt", nil), persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", p.SubjectID)}, Any: [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(time.Now())}}}})
		if err != nil {
			return err
		}
		out.Requestable, out.RequestableTruncated, err = requestablePermissions(ctx, a.store, p.OwnerID)
		if err != nil {
			return err
		}
		repo := resource.NewRepository(a.store)
		for i := range out.Requestable {
			item := &out.Requestable[i]
			if item.Kind == resource.Network && item.SandboxID == "local" {
				binding, err := repo.Binding(ctx, p.OwnerID, item.ResourceID)
				if err != nil {
					return err
				}
				item.Network = describeNetwork(localNetworkInfo(a), binding.SandboxTarget)
			}
		}
		candidates := map[string]resourceCapability{}
		now := time.Now()
		for _, g := range grants {
			if g.OwnerID != p.OwnerID || g.AssistantID != p.SubjectID || g.RevokedAt != nil || (g.ExpiresAt != nil && !g.ExpiresAt.After(now)) {
				continue
			}
			r, err := repo.Get(ctx, p.OwnerID, g.ResourceID)
			if persistence.Missing(err) {
				continue
			}
			if err != nil {
				return err
			}
			paths := []string{""}
			if r.Kind == resource.Storage {
				paths = g.Scope.PathPrefixes
				if len(paths) == 0 {
					paths = []string{"/"}
				}
			}
			for _, name := range paths {
				item := resourceCapability{ResourceID: r.ID, Name: r.Name, Kind: r.Kind, Source: "user_grant", Path: name, ModelID: g.Scope.ModelID, Operations: []resourceOperation{}}
				if r.Kind == resource.Storage {
					item.PathMeaning = "resource_subtree"
					if name == "/" {
						item.PathMeaning = "resource_root_not_host_root"
					}
					item.ShellPath = resourceShellPath(r.ID, name)
					item.FilePath = "/resources/" + string(r.ID) + name
					if r.Provider == "managed-workspace" {
						item.ShellPath = "/work" + strings.TrimSuffix(name, "/")
						item.FilePath = item.ShellPath
						item.PathMeaning = "assistant_workspace"
					}
				}
				if r.Kind == resource.Storage && sandboxOf(r) == "local" && platform.NativeSandbox() {
					if r.Provider == "managed-workspace" {
						item.ShellPath = filepath.Join(platform.AssistantWorkspace(a.dataDir, a.storageConfig.WorkRoot, p.OwnerID, p.SubjectID), strings.TrimPrefix(name, "/"))
					} else {
						b, err := repo.Binding(ctx, p.OwnerID, r.ID)
						if err != nil {
							return err
						}
						item.ShellPath = filepath.Join(b.RootPath, strings.TrimPrefix(name, "/"))
					}
				}
				if r.Kind == resource.Model {
					b, err := repo.Binding(ctx, p.OwnerID, r.ID)
					if err != nil {
						return err
					}
					for _, entry := range modelcatalog.Entries(r, b) {
						if entry.ID == item.ModelID {
							item.ModelName = entry.Config.Name
						}
					}
				}
				key := string(r.ID) + "\x00" + name + "\x00" + string(item.ModelID)
				item.Grants = append(candidates[key].Grants, permissionGrant{g.Version, g.Constraints, g.Temporary, g.ID, g.Actions, g.Scope, g.ExpiresAt})
				candidates[key] = item
			}
		}
		// Establish compute availability independently of page truncation/order.
		computeIDs := []core.ID{}
		for _, item := range candidates {
			if item.Kind != resource.Compute {
				continue
			}
			r, err := repo.Get(ctx, p.OwnerID, item.ResourceID)
			if err != nil {
				return err
			}
			ok, err := c.allowed(ctx, p, item, policy.Execute)
			if err != nil {
				return err
			}
			if ok && r.Provider == "local-sandbox" {
				computeIDs = append(computeIDs, r.ID)
			}
		}
		sort.Slice(computeIDs, func(i, j int) bool { return computeIDs[i] < computeIDs[j] })
		executionReason := shellReason
		if len(computeIDs) == 0 {
			executionReason = "compute_permission_required"
		}
		workspaceOps := []resourceOperation{
			{policy.Read, "file_search", true, ""}, {policy.Read, "file_read", true, ""}, {policy.Read, "file_list", true, ""},
			{policy.Write, "file_write", true, ""}, {policy.Write, "file_edit", true, ""}, {policy.Write, "file_move", true, ""}, {policy.Write, "file_delete", true, ""},
			{policy.Read, "shell_execute", executionReason == "", executionReason}, {policy.Write, "shell_execute", executionReason == "", executionReason},
		}
		out.BuiltInStorage = append(out.BuiltInStorage, builtinStorage{Kind: "workspace", Source: "runtime", Operations: workspaceOps, Path: "/work", Lifetime: "assistant", Available: true, Actions: []policy.Action{policy.Read, policy.Write}, ShellComputeIDs: computeIDs})
		temporaryActions := []policy.Action{}
		if executionReason == "" {
			temporaryActions = []policy.Action{policy.Read, policy.Write}
		}
		out.BuiltInStorage = append(out.BuiltInStorage, builtinStorage{Kind: "temporary", Source: "runtime", Operations: []resourceOperation{{policy.Read, "shell_execute", executionReason == "", executionReason}, {policy.Write, "shell_execute", executionReason == "", executionReason}}, Path: "/tmp", Lifetime: "execution", Available: executionReason == "", Reason: executionReason, Actions: temporaryActions, ShellComputeIDs: computeIDs})
		globalOps := []resourceOperation{}
		for _, op := range workspaceOps {
			if op.Action == policy.Read {
				globalOps = append(globalOps, op)
			}
		}
		out.BuiltInStorage = append(out.BuiltInStorage, builtinStorage{Kind: "global", Source: "runtime", Operations: globalOps, Path: "/global", Lifetime: "owner", Available: true, Actions: []policy.Action{policy.Read}, ShellComputeIDs: computeIDs})
		keys := make([]string, 0, len(candidates))
		for key := range candidates {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if len(out.Resources) == out.Limit {
				out.Truncated = true
				break
			}
			item := candidates[key]
			r, err := repo.Get(ctx, p.OwnerID, item.ResourceID)
			if err != nil {
				return err
			}
			for _, action := range []policy.Action{policy.Read, policy.Write, policy.Execute} {
				if (item.Kind == resource.Storage && action == policy.Execute) || (item.Kind != resource.Storage && action == policy.Write) || ((item.Kind == resource.Compute || item.Kind == resource.Network) && action == policy.Read) {
					continue
				}
				allowed, err := c.allowed(ctx, p, item, action)
				if err != nil {
					return err
				}
				reason := ""
				if !allowed {
					reason = "permission_required"
				}
				if r.State != resource.Available {
					reason = "resource_unavailable"
				}
				via := "model"
				switch item.Kind {
				case resource.Storage:
					via = "file_" + string(action)
					if reason == "" {
						if _, err := a.storageBinding(ctx, p, item.ResourceID, item.Path, action); err != nil {
							reason = string(core.ReasonOf(err))
							if reason == "" {
								reason = "storage_unavailable"
							}
						}
					}
				case resource.Compute:
					via = "shell_execute"
					if reason == "" {
						reason = shellReason
					}
					if r.Provider != "local-sandbox" {
						reason = "unsupported_resource"
					}
				case resource.Network:
					via = "shell_execute"
					if reason == "" {
						reason = executionReason
					}
					b, err := repo.Binding(ctx, p.OwnerID, r.ID)
					if err != nil {
						return err
					}
					if sandboxOf(r) == "local" {
						item.Network = describeNetwork(localNetworkInfo(a), b.SandboxTarget)
					}
					configured := false
					for _, n := range a.storageConfig.NetworkTargets(a.ownPort) {
						if n.Name == b.SandboxTarget {
							configured = true
						}
					}
					if !configured || r.Provider != "sandbox-network" {
						reason = "network_disabled"
					}
				case resource.Model:
					if item.ModelName == "" {
						reason = "model_unavailable"
					}
				default:
					reason = "unsupported_resource"
				}
				item.Operations = append(item.Operations, resourceOperation{action, via, reason == "", reason})
				if item.Kind == resource.Storage {
					if action == policy.Read {
						item.Operations = append(item.Operations, resourceOperation{action, "file_list", reason == "", reason}, resourceOperation{action, "file_search", reason == "", reason})
					}
					if action == policy.Write {
						editReason := reason
						if editReason == "" {
							if _, err := a.storageBinding(ctx, p, item.ResourceID, item.Path, policy.Read); err != nil {
								editReason = "read_permission_required_for_edit"
							}
						}
						item.Operations = append(item.Operations, resourceOperation{action, "file_edit", editReason == "", editReason}, resourceOperation{action, "file_move", reason == "", reason}, resourceOperation{action, "file_delete", reason == "", reason})
					}
					if reason == "" {
						reason = executionReason
					}
					if action == policy.Write && reason == "" {
						// A read/write mount cannot be derived from a write-only grant.
						if _, err := a.storageBinding(ctx, p, item.ResourceID, item.Path, policy.Read); err != nil {
							reason = "read_permission_required_for_mount"
						}
					}
					item.Operations = append(item.Operations, resourceOperation{action, "shell_execute", reason == "", reason})
				}
			}
			out.Resources = append(out.Resources, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (c resourceCatalog) allowed(ctx context.Context, p core.Principal, item resourceCapability, action policy.Action) (bool, error) {
	destination := item.Path
	if destination == "" {
		destination = "/"
	}
	err := c.adapters.policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: item.ResourceID}, ResourceID: item.ResourceID, ModelID: item.ModelID, Action: action, Destination: destination})
	if err == nil {
		return true, nil
	}
	if code := core.CodeOf(err); code == core.Forbidden || code == core.NotFound {
		return false, nil
	}
	return false, err
}
