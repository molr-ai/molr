package app

import (
	"context"
	"molr.ai/runtime/internal/core"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/sandboxruntime"
)

type remoteSandboxHost struct{ router *sandboxRouter }

func (h remoteSandboxHost) Connect(ctx context.Context, p core.Principal, id core.ID) (sandboxruntime.Client, sm.Info, error) {
	c, err := h.router.connection(ctx, p, id)
	if err != nil {
		return nil, sm.Info{}, err
	}
	client, err := h.router.client(ctx, p, c)
	return client, c.View.Info, err
}
func (h remoteSandboxHost) Info(ctx context.Context, p core.Principal, id core.ID) (sm.Info, error) {
	return h.router.info(ctx, p, id)
}
func (h remoteSandboxHost) Epoch(ctx context.Context, owner core.ID) (uint64, error) {
	return h.router.epoch(ctx, owner)
}
func (h remoteSandboxHost) CheckExecution(ctx context.Context, e core.Execution) error {
	return (access{h.router.adapters.store}).execution(ctx, e)
}
func (h remoteSandboxHost) CheckConnection(ctx context.Context, p core.Principal, id core.ID) error {
	_, err := h.router.connection(ctx, p, id)
	return err
}
func (h remoteSandboxHost) CheckMembership(ctx context.Context, owner, id core.ID) error {
	_, err := groupReference(ctx, h.router.adapters.store, owner, "sandbox", id)
	return err
}
