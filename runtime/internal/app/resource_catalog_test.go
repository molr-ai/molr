package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/modelcatalog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
)

type resourceAccessFixture struct {
	b                *Bundle
	owner            core.Principal
	execution        core.Execution
	storage, compute resource.Resource
	ad               adapters
	catalog          resourceCatalog
	root             string
}

func newResourceAccessFixture(t *testing.T, configure func(*config.Config, string)) resourceAccessFixture {
	t.Helper()
	cfg := testConfig(t)
	root := t.TempDir()
	for _, dir := range []string{"read", "write", "secret"} {
		need(t, os.Mkdir(filepath.Join(root, dir), 0700))
		need(t, os.WriteFile(filepath.Join(root, dir, "data"), []byte("original"), 0600))
	}
	if configure != nil {
		configure(&cfg, root)
	}
	b, err := Build(t.Context(), cfg)
	need(t, err)
	t.Cleanup(func() { b.Close() })
	owner, _ := ownerSession(t, b)
	e := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: owner.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(owner.OwnerID, "job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "job", OwnerID: owner.OwnerID, AssistantID: e.AssistantID}})
	}))
	storage, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "storage"}, Kind: resource.Storage, Provider: "local-directory", Name: "Molr", Binding: resource.Binding{RootPath: root}})
	need(t, err)
	var compute resource.Resource
	if cfg.Sandbox.Enabled {
		compute, err = b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Local compute"})
		need(t, err)
	}
	permissions := b.Services.Grants.(policy.Authorizer)
	ad := adapters{store: b.store, policy: permissions, dataDir: cfg.DataDir, storageConfig: cfg.Sandbox}
	f := resourceAccessFixture{b, owner, e, storage, compute, ad, resourceCatalog{ad, b.sandbox}, root}
	if cfg.Sandbox.Enabled {
		f.grant(t, compute.ID, "compute", []policy.Action{policy.Execute}, policy.Scope{}, nil)
	}
	return f
}

func (f resourceAccessFixture) grant(t *testing.T, id core.ID, key string, actions []policy.Action, scope policy.Scope, expires *time.Time) policy.Grant {
	t.Helper()
	r, err := resource.NewRepository(f.b.store).Get(t.Context(), f.owner.OwnerID, id)
	need(t, err)
	constraints := testResourceConstraints(r.Kind)
	g, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{Constraints: constraints, Mutation: core.Mutation{IdempotencyKey: key}, AssistantID: f.execution.AssistantID, ResourceID: id, Actions: actions, Scope: scope, ExpiresAt: expires})
	need(t, err)
	return g
}
func (f resourceAccessFixture) list(t *testing.T) resourceListing {
	t.Helper()
	data, err := f.catalog.list(t.Context(), f.execution.Principal)
	need(t, err)
	var out resourceListing
	need(t, json.Unmarshal(data, &out))
	// Seatbelt executes against the authorized host path instead of a Linux mount.
	sanitized := out
	sanitized.Resources = append([]resourceCapability(nil), out.Resources...)
	for i := range sanitized.Resources {
		item := &sanitized.Resources[i]
		if platform.NativeSandbox() && item.Kind == resource.Storage && item.ResourceID == f.storage.ID {
			want := filepath.Join(textfiles.SystemPath(f.root), strings.TrimPrefix(item.Path, "/"))
			if textfiles.SystemPath(item.ShellPath) != want {
				t.Fatalf("native shell path = %q, want %q", item.ShellPath, want)
			}
			item.ShellPath = ""
		}
	}
	data, err = json.Marshal(sanitized)
	need(t, err)
	if strings.Contains(string(data), f.root) || strings.Contains(string(data), f.ad.dataDir) || strings.Contains(string(data), "/molr") {
		t.Fatal("host/internal paths leaked outside authorized shell paths")
	}
	return out
}
func lookupOperation(t *testing.T, out resourceListing, id core.ID, path string, action policy.Action, via string) resourceOperation {
	t.Helper()
	for _, r := range out.Resources {
		if r.ResourceID == id && r.Path == path {
			for _, op := range r.Operations {
				if op.Action == action && op.Via == via {
					return op
				}
			}
		}
	}
	t.Fatalf("missing operation %s %s %s", path, action, via)
	return resourceOperation{}
}

func TestResourceCatalogSeparatesScopeAndRuntimeStorage(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	f.grant(t, f.storage.ID, "read", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/read"}}, nil)
	f.grant(t, f.storage.ID, "write", []policy.Action{policy.Read, policy.Write}, policy.Scope{PathPrefixes: []string{"/write"}}, nil)
	out := f.list(t)
	if lookupOperation(t, out, f.storage.ID, "/read", policy.Write, "file_write").Available {
		t.Fatal("write grant spread to read-only scope")
	}
	if !lookupOperation(t, out, f.storage.ID, "/write", policy.Write, "file_write").Available {
		t.Fatal("authorized write unavailable")
	}
	if len(out.BuiltInStorage) != 3 || out.BuiltInStorage[0].Source != "runtime" || out.BuiltInStorage[0].Lifetime != "assistant" || out.BuiltInStorage[1].Lifetime != "execution" {
		t.Fatal("builtin lifetime/origin ambiguous")
	}
	for _, item := range out.Resources {
		if item.Source != "user_grant" || item.Path == "/secret" {
			t.Fatal("unexpected capability")
		}
		if !platform.NativeSandbox() && item.Kind == resource.Storage && item.ShellPath != resourceShellPath(item.ResourceID, item.Path) {
			t.Fatal("unstable shell mapping")
		}
	}
	if _, err := (newResourceFileTools(resourceFileOptions{adapters: f.ad})).Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: tools.FileTarget{ResourceID: f.storage.ID, Path: "/read/data"}, Text: "changed", Overwrite: true}); err == nil {
		t.Fatal("file tool expanded write scope")
	}
	_, err := (newResourceFileTools(resourceFileOptions{adapters: f.ad})).Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: tools.FileTarget{ResourceID: f.storage.ID, Path: "/write/data"}, Text: "changed", Overwrite: true})
	need(t, err)
	plan, err := f.b.sandbox.plan(t.Context(), f.execution, tools.ShellRequest{ComputeID: f.compute.ID, Script: "true", Files: []tools.ShellFile{{ResourceID: f.storage.ID, Path: "/write", Write: true}, {ResourceID: f.storage.ID, Path: "/read"}}})
	need(t, err)
	if plan.Mounts[0].Destination != resourceShellPath(f.storage.ID, "/write") || plan.Mounts[1].Destination != resourceShellPath(f.storage.ID, "/read") {
		t.Fatal("mapping depends on file order")
	}
	_, err = f.b.sandbox.plan(t.Context(), f.execution, tools.ShellRequest{ComputeID: f.compute.ID, Script: "true", Files: []tools.ShellFile{{ResourceID: f.storage.ID, Path: "/read", Write: true}}})
	if err == nil {
		t.Fatal("Shell expanded write scope")
	}
}

func TestStorageDeploymentRestrictionsApplyToDiscoveryFilesAndShell(t *testing.T) {
	for _, mode := range []string{"readonly", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := newResourceAccessFixture(t, nil)
			f.grant(t, f.storage.ID, "all", []policy.Action{policy.Read, policy.Write}, policy.Scope{}, nil)
			// Tighten deployment after resources and grants already exist.
			if mode == "readonly" {
				f.ad.storageConfig.Directories = []config.SandboxDirectory{{Path: f.root, ReadOnly: true}}
			} else {
				f.ad.storageConfig.RestrictDirectories = true
			}
			f.catalog.adapters = f.ad
			f.b.sandbox.config = f.ad.storageConfig

			out := f.list(t)
			op := lookupOperation(t, out, f.storage.ID, "/", policy.Write, "file_write")
			want := "storage_read_only"
			if mode == "disabled" {
				want = "storage_disabled"
			}
			if op.Available || op.Reason != want {
				t.Fatalf("wrong restriction: %+v", op)
			}
			for _, item := range out.Resources {
				if item.ResourceID == f.storage.ID && item.PathMeaning != "resource_root_not_host_root" {
					t.Fatal("ambiguous root namespace")
				}
			}
			_, err := (newResourceFileTools(resourceFileOptions{adapters: f.ad})).Write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: tools.FileTarget{ResourceID: f.storage.ID, Path: "/write/data"}, Text: "bad", Overwrite: true})
			if string(core.ReasonOf(err)) != want {
				t.Fatalf("file bypass: %v", err)
			}
			_, err = f.b.sandbox.plan(t.Context(), f.execution, tools.ShellRequest{ComputeID: f.compute.ID, Script: "true", Files: []tools.ShellFile{{ResourceID: f.storage.ID, Path: "/write", Write: true}}})
			if string(core.ReasonOf(err)) != want {
				t.Fatalf("shell bypass: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(f.root, "write/data"))
			need(t, err)
			if string(data) != "original" {
				t.Fatal("unauthorized side effect")
			}
		})
	}
}

func TestResourceCatalogRevocationIdentityAndWriteOnlyGrant(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	g := f.grant(t, f.storage.ID, "write", []policy.Action{policy.Write}, policy.Scope{PathPrefixes: []string{"/write"}}, nil)
	out := f.list(t)
	if !lookupOperation(t, out, f.storage.ID, "/write", policy.Write, "file_write").Available || lookupOperation(t, out, f.storage.ID, "/write", policy.Read, "file_read").Available {
		t.Fatal("write-only grant misrepresented")
	}
	_, err := f.catalog.list(t.Context(), f.owner)
	if err == nil {
		t.Fatal("owner inherited assistant discovery")
	}
	other := f.execution.Principal
	other.SubjectID = "other"
	if _, err = f.catalog.list(t.Context(), other); err == nil {
		t.Fatal("cross-assistant discovery")
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: g.Version}, GrantID: g.ID}))
	out = f.list(t)
	for _, item := range out.Resources {
		if item.ResourceID == f.storage.ID {
			t.Fatal("revoked resource still advertised")
		}
	}
}

func TestResourceMountIdentityAndOverlap(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	f.grant(t, f.storage.ID, "all", []policy.Action{policy.Read, policy.Write}, policy.Scope{}, nil)
	for _, paths := range [][]string{{"/", "/read"}, {"/read", "/"}, {"/read", "/read"}} {
		req := tools.ShellRequest{ComputeID: f.compute.ID, Script: "true"}
		for _, p := range paths {
			req.Files = append(req.Files, tools.ShellFile{ResourceID: f.storage.ID, Path: p})
		}
		if _, err := f.b.sandbox.plan(t.Context(), f.execution, req); core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("overlap accepted: %v", err)
		}
	}
	if resourceShellPath("one", "/read") == resourceShellPath("two", "/read") {
		t.Fatal("resource identity collision")
	}
	for _, p := range []string{"../escape", "/read/../secret", "//read", "/read\\secret"} {
		if _, err := resourcePath(p); err == nil {
			t.Fatalf("unsafe path %q accepted", p)
		}
	}
}

func TestResourceCatalogFiltersModelsExpiryAndOfflineResources(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	binding := resource.Binding{Endpoint: "https://models.example/chat/completions", AllowedOrigins: []string{"https://models.example"}, Models: []resource.ModelConfig{{Name: "allowed-model", ContextLimit: 8192}, {Name: "ungranted-model", ContextLimit: 8192}}}
	r, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "models"}, Kind: resource.Model, Provider: "openai-compatible", Name: "provider", Binding: binding})
	need(t, err)
	id := modelcatalog.Entries(r, binding)[0].ID
	f.grant(t, r.ID, "model", []policy.Action{policy.Read, policy.Execute}, policy.Scope{ModelID: id}, nil)
	read := f.grant(t, f.storage.ID, "read", []policy.Action{policy.Read}, policy.Scope{}, nil)
	out := f.list(t)
	models := 0
	for _, item := range out.Resources {
		if item.Kind == resource.Model {
			models++
			if item.ModelID != id || item.ModelName != "allowed-model" {
				t.Fatal("model grant became provider-wide access")
			}
		}
	}
	if models != 1 {
		t.Fatal("authorized model absent")
	}
	past := time.Now().Add(-time.Minute)
	read.ExpiresAt = &past
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "security.grants", persistence.Key(f.owner.OwnerID, read.ID), read)
	}))
	out = f.list(t)
	for _, item := range out.Resources {
		if item.ResourceID == f.storage.ID {
			t.Fatal("expired storage advertised")
		}
	}
	r.State = resource.Offline
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.resources", persistence.Key(catalog.Scope, r.ID), r)
	}))
	out = f.list(t)
	op := lookupOperation(t, out, r.ID, "", policy.Execute, "model")
	if op.Available || op.Reason != "resource_unavailable" {
		t.Fatal("offline model advertised available")
	}
	// The catalog remains useful when command execution is disabled.
	f.catalog.shell = nil
	out = f.list(t)
	if !out.BuiltInStorage[0].Available || out.BuiltInStorage[1].Available || out.BuiltInStorage[1].Reason != "execution_disabled" {
		t.Fatal("builtin storage usable without execution environment")
	}
}

func TestPermissionListInstalledWithoutShell(t *testing.T) {
	cfg := testConfig(t)
	cfg.Sandbox.Enabled = false
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	owner, _ := ownerSession(t, b)
	id := core.ID("assistant")
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, id), assistant.Assistant{ID: id, OwnerID: owner.OwnerID, State: assistant.Active, Version: 1})
	}))
	need(t, b.Services.Agent.(*agent.Engine).Lifecycle(t.Context(), owner, id, "initialize"))
	caps, err := b.Services.Agent.Capabilities(t.Context(), owner, id)
	need(t, err)
	found := false
	for _, tool := range caps.Tools {
		if tool.Name == "permission_list" {
			found = tool.Available
		}
		if (tool.Name == "shell_execute" && tool.Available) || tool.Name == "sandbox_resources" {
			t.Fatal("disabled or obsolete tool advertised")
		}
	}
	if !found {
		t.Fatal("resource discovery coupled to Shell availability")
	}
}

func testResourceConstraints(kind resource.Kind) *policy.Constraints {
	if platform.NativeSandbox() {
		if kind == resource.Compute {
			return &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 1800}}
		}
		return nil
	}
	switch kind {
	case resource.Compute:
		return &policy.Constraints{Compute: &policy.ComputeConstraints{CPUPercent: 100, MemoryBytes: 512 << 20, Processes: 64, TempBytes: 64 << 20, MaxDurationSeconds: 1800}}
	case resource.Network:
		return &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 64 << 20, Connections: 16, Period: "day", Timezone: "UTC"}}
	}
	return nil
}

func TestComponentDirectoriesCannotBeGrantedThroughParentResources(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	protected := filepath.Join(f.root, "components")
	need(t, os.Mkdir(protected, 0700))
	f.ad.storageConfig.ProtectedRoots = []string{protected}
	for _, root := range []string{f.root, protected} {
		if _, err := f.ad.providers()["local-directory"].Validate(t.Context(), resource.Binding{RootPath: root}); core.CodeOf(err) != core.Forbidden {
			t.Fatalf("installation path accepted: %s %v", root, err)
		}
	}
}

func testNetworkPreset() string {
	if platform.NativeSandbox() {
		return "preset:network"
	}
	return "preset:internet"
}
