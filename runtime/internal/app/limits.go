package app

import (
	"context"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/persistence"
)

type modelLimit struct {
	model.Client
	gate chan struct{}
}

func (m modelLimit) Generate(ctx context.Context, r model.Request) (model.Stream, error) {
	select {
	case m.gate <- struct{}{}:
		defer func() { <-m.gate }()
		return m.Client.Generate(ctx, r)
	case <-ctx.Done():
		return nil, persistence.CheckContext(ctx)
	}
}

type toolLimit struct {
	tools.Executor
	gate chan struct{}
}

func (t toolLimit) Execute(ctx context.Context, c tools.Call) (tools.Result, error) {
	select {
	case t.gate <- struct{}{}:
		defer func() { <-t.gate }()
		return t.Executor.Execute(ctx, c)
	case <-ctx.Done():
		err := persistence.CheckContext(ctx)
		return tools.ErrorResult(c, err), err
	}
}
