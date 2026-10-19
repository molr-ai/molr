package app

import (
	"context"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

const sandboxListConcurrency = 4

func (s *sandboxRouter) listSandboxes(ctx context.Context, p core.Principal) ([]sandboxView, error) {
	rows, err := persistence.SelectCatalog[sandboxConnection](ctx, s.adapters.store, "sandbox.connections", p.OwnerID, persistence.Query{})
	if err != nil {
		return nil, err
	}
	connections := []sandboxConnection{}
	for _, c := range rows {
		if catalogOwned(ctx, s.adapters.store, c.OwnerID, p.OwnerID) {
			connections = append(connections, c)
		}
	}
	out := make([]sandboxView, len(connections)+1)
	jobs := make(chan int, len(out))
	for i := range out {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(sandboxListConcurrency, len(out)) {
		workers.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				if i == 0 {
					v := sandboxView{ID: "local", Name: "Local sandbox", Local: true, State: "available", Version: 1, Info: s.localInfo(ctx), CheckedAt: time.Now()}
					if !v.Info.Enabled {
						v.State = "disabled"
					}
					out[i] = v
				} else {
					out[i] = s.checkSandbox(ctx, p, connections[i-1])
				}
			}
		})
	}
	workers.Wait()
	if err := persistence.CheckContext(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *sandboxRouter) checkSandbox(ctx context.Context, p core.Principal, c sandboxConnection) sandboxView {
	v := c.View
	v.CheckedAt = time.Now()
	if v.State == "disconnected" {
		return v
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client, err := s.client(check, p, c)
	if err == nil {
		v.Info, err = client.Info(check)
	}
	if err != nil {
		v.State = "offline"
		v.ConnectionReason = string(sandboxDiagnosis(err).Reason)
		v.Info = c.View.Info
		v.Info.Enabled = false
		v.Info.Storage = false
		v.Info.Capabilities = platform.Capabilities{OS: v.Info.Capabilities.OS, Reason: "sandbox_offline"}
	}
	return v
}
