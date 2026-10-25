package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/filetools"
	"molr.ai/runtime/internal/media"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type resourceFileOptions struct {
	router           *sandboxRouter
	adapters         adapters
	resolveOverride  func(context.Context, core.Execution, tools.FileTarget, ...policy.Action) (string, string, error)
	providerOverride func(context.Context) model.Provider
}
type resourceFileTools struct {
	*filetools.Tools
	resourceFileOptions
}

func newResourceFileTools(o resourceFileOptions) *resourceFileTools {
	f := &resourceFileTools{resourceFileOptions: o}
	f.Tools = filetools.New(filetools.Options{
		DataDir: o.adapters.dataDir, Resolve: f.resolve, CheckSize: f.checkWorkspaceFileSize,
		Remote: func(ctx context.Context, e core.Execution, kind string, target tools.FileTarget, request any, modelID core.ID) (json.RawMessage, bool, error) {
			if f.router == nil {
				return nil, false, nil
			}
			return f.router.file(ctx, e, kind, target, request, modelID)
		},
		Media: func(ctx context.Context, e core.Execution, p media.Part) ([]byte, bool, error) {
			if f.router == nil {
				return nil, false, nil
			}
			return f.router.media(ctx, e, p)
		},
		Provider: func(ctx context.Context, e core.Execution, id core.ID) (model.Provider, error) {
			if f.providerOverride != nil {
				return f.providerOverride(ctx), nil
			}
			if id == "" {
				return model.Provider{}, nil
			}
			provider, err := f.adapters.provider(ctx, e.Principal, id)
			if err != nil {
				return provider, err
			}
			err = f.adapters.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: provider.Model.ResourceID}, ResourceID: provider.Model.ResourceID, ModelID: id, Action: policy.Execute, Destination: provider.Endpoint})
			return provider, err
		},
	})
	return f
}
func (f *resourceFileTools) resolve(ctx context.Context, e core.Execution, target tools.FileTarget, actions ...policy.Action) (string, string, error) {
	if err := filetools.CheckPolicy(ctx, e, target, actions...); err != nil {
		return "", "", err
	}
	if filetools.IsBuiltin(target) {
		return f.resolveBuiltin(ctx, e, target, actions...)
	}
	if f.resolveOverride != nil {
		return f.resolveOverride(ctx, e, target, actions...)
	}
	if f.router != nil {
		id, err := f.router.location(ctx, e, target)
		if err != nil {
			return "", "", err
		}
		if id != "local" {
			return "", "", core.Invalid("sandbox", "remote paths must use sandbox storage API")
		}
	}
	if err := (access{f.adapters.store}).execution(ctx, e); err != nil {
		return "", "", err
	}
	if target.ResourceID == "" && (target.Path == "/global" || strings.HasPrefix(target.Path, "/global/")) {
		if targetSandbox(target.SandboxID) != "local" || slices.Contains(actions, policy.Write) {
			return "", "", core.NewError(core.Forbidden, "global files are read-only for assistants")
		}
		root := platform.UserGlobal(f.adapters.dataDir, e.Principal.OwnerID)
		if err := os.MkdirAll(root, 0700); err != nil {
			return "", "", err
		}
		dir, err := openResourceDirectory(root, ".")
		if err != nil {
			return "", "", err
		}
		dir.Close()
		name := strings.TrimPrefix(strings.TrimPrefix(target.Path, "/global"), "/")
		if name == "" {
			name = "."
		}
		if !filepath.IsLocal(name) || path.Clean(name) != name {
			return "", "", core.Invalid("path", "canonical global path required")
		}
		return root, name, nil
	}
	if target.FullAccess {
		router := f.router
		if router == nil {
			router = &sandboxRouter{adapters: f.adapters}
		}
		if err := router.requireFullAccess(ctx, e, "local"); err != nil {
			return "", "", err
		}
		if target.ResourceID != "" {
			r, b, err := f.adapters.binding(ctx, e.Principal, target.ResourceID)
			if err != nil {
				return "", "", err
			}
			if r.Kind != resource.Storage || sandboxOf(r) != "local" {
				return "", "", denied()
			}
			if r.Provider == "managed-workspace" {
				target.Path = "/work/" + strings.TrimPrefix(target.Path, "/")
			} else {
				target.Path = filepath.Join(b.RootPath, strings.TrimPrefix(target.Path, "/"))
			}
		}
		return directFilePath(f.adapters.dataDir, f.adapters.storageConfig.WorkRoot, e, target)
	}
	if target.ResourceID != "" {
		r, err := resource.NewRepository(f.adapters.store).Get(ctx, e.Principal.OwnerID, target.ResourceID)
		if err != nil {
			return "", "", err
		}
		if r.Provider == "managed-workspace" {
			relative, err := resourcePath(target.Path)
			if err != nil {
				return "", "", err
			}
			target.ResourceID = ""
			target.Path = "/work" + strings.TrimSuffix(relative, "/")
		}
	}
	if target.ResourceID == "" {
		name, err := workspaceRelativePath(target.Path)
		if err != nil {
			return "", "", err
		}
		root := platform.AssistantWorkspace(f.adapters.dataDir, f.adapters.storageConfig.WorkRoot, e.Principal.OwnerID, e.AssistantID)
		spec, err := workspaceConstraints(ctx, f.adapters.store, e.Principal.OwnerID, e.AssistantID, "local")
		if err != nil {
			return "", "", err
		}
		if slices.Contains(actions, policy.Write) {
			if err := applyWorkspaceQuota(root, spec, f.adapters.storageConfig.WorkspaceProjectQuotas); err != nil {
				return "", "", err
			}
		}
		if err := platform.EnsureWorkspace(root); err != nil {
			return "", "", err
		}
		return root, name, nil
	}
	name, err := resourcePath(target.Path)
	if err != nil {
		return "", "", err
	}
	root := ""
	for _, action := range actions {
		b, err := f.adapters.storageBinding(ctx, e.Principal, target.ResourceID, name, action)
		if err != nil {
			return "", "", err
		}
		root = b.RootPath
	}
	return root, strings.TrimPrefix(name, "/"), nil
}
