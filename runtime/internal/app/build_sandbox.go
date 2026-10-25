package app

import (
	"context"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/maintenance"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/shellsession"
	"time"
)

type sandboxServices struct {
	router    *sandboxRouter
	files     *resourceFileTools
	resources resource.Service
	grants    policy.Service
}

func (b *Bundle) buildSandbox(cfg config.Config, key []byte, adapters adapters, permissions *policy.Manager, identity *auth.Manager, vault *credential.Vault, auditService audit.Service, maintenance maintenance.Service) (sandboxServices, error) {
	store := adapters.store
	var err error
	providers := adapters.providers()
	for name, provider := range sandboxProviders(cfg) {
		providers[name] = provider
	}
	var resources resource.Service = resource.NewService(store, resource.Options{Authorizer: permissions, Providers: providers, Credentials: vault, CheckVerification: identity.CheckVerification, OnDeleteProvider: adapters.deleteProviderReferences})
	var grants policy.Service = permissions
	if cfg.Sandbox.Enabled {
		b.sandbox, err = newShellService(store, permissions, cfg)
		if err != nil {
			return sandboxServices{}, err
		}
		b.sandbox.audit = auditService
		provider := providers["local-sandbox"]
		provider.Check = func(ctx context.Context, _ resource.Binding) (resource.Status, error) {
			caps, err := b.sandbox.runner.Capabilities(ctx)
			status := resource.Status{State: resource.Available, ReasonCode: caps.Reason, CheckedAt: time.Now()}
			if !caps.Shell && !caps.FullAccess {
				status.State = resource.Offline
			}
			return status, err
		}
		providers["local-sandbox"] = provider
		grants = sandboxGrants{Service: permissions, shell: b.sandbox}
		resources = sandboxResources{Service: resources, shell: b.sandbox}
	}
	sandboxRouter, err := newSandboxRouter(adapters, b.sandbox, key)
	if err != nil {
		return sandboxServices{}, err
	}
	sandboxRouter.verify = identity.CheckVerification
	fileTools := newResourceFileTools(resourceFileOptions{adapters: adapters, router: sandboxRouter})
	sandboxRouter.files = fileTools
	b.remote = sandboxRouter
	b.shellTasks = shellsession.New(store, shellTaskEnvironment{sandboxRouter}, shellsession.Options{
		Identity: identity, Forward: cfg.PortForward, ForwardHTTP: cfg.HTTP, Browser: cfg.Browser, Maintenance: maintenance,
		ReadScreenshot: fileTools.Read,
	})
	grants = remoteSandboxGrants{Service: grants, router: sandboxRouter}
	resources = remoteSandboxResources{Service: resources, router: sandboxRouter}
	return sandboxServices{sandboxRouter, fileTools, resources, grants}, nil
}
