package plugins

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"sort"
	"time"
)

type HTTPCallView struct {
	ID           core.ID
	OwnerID      core.ID
	ProviderID   string
	ConnectionID core.ID
	State        string
	CreatedAt    time.Time
	DurationMS   int64
	StatusCode   int
	Failure      string
	Test         bool
	Usage        any
}

func (s *Service) HTTPHistory(ctx context.Context, p core.Principal, q ConnectionRequest) ([]HTTPCallView, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return nil, err
	}
	c, err := s.connection(ctx, p.OwnerID, q.ID)
	if err != nil {
		return nil, err
	}
	if c.PluginID != q.PluginID {
		return nil, core.NewError(core.NotFound, "connection not found")
	}
	rows, err := persistence.Select[HTTPCall](ctx, s.store, "plugins.http-calls", persistence.Query{Where: []persistence.Filter{persistence.Eq("Connection.ID", c.ID)}, OrderBy: "CreatedAtKey", Descending: true, Limit: 100})
	if err != nil {
		return nil, err
	}
	out := []HTTPCallView{}
	for _, r := range rows {
		if r.Connection.ID != c.ID {
			continue
		}
		var result struct {
			Usage any `json:"usage"`
		}
		_ = json.Unmarshal(r.Result, &result)
		out = append(out, HTTPCallView{ID: r.ID, OwnerID: r.OwnerID, ProviderID: r.ProviderID, ConnectionID: r.Connection.ID, State: r.State, CreatedAt: r.CreatedAt, DurationMS: r.DurationMS, StatusCode: r.StatusCode, Failure: r.Failure, Test: r.Test, Usage: result.Usage})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}

type AcknowledgeCall struct{ ConnectionID, OwnerID, ID core.ID }

// Acknowledgement only marks history reviewed. Local concurrency is already
// released on return; this does not assert upstream success/failure or permit replay.
func (s *Service) AcknowledgeHTTP(ctx context.Context, p core.Principal, q AcknowledgeCall) error {
	if err := s.authorize(ctx, p, true); err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := s.connection(ctx, p.OwnerID, q.ConnectionID)
		if err != nil {
			return err
		}
		key := persistence.Key(q.OwnerID, q.ID)
		r, err := persistence.Load[HTTPCall](ctx, s.store, "plugins.http-calls", key)
		if err != nil {
			return err
		}
		if r.Connection.ID != c.ID {
			return core.NewError(core.Forbidden, "call does not belong to connection")
		}
		if r.State == "acknowledged" {
			return nil
		}
		if r.State != "unknown" {
			return core.NewError(core.Conflict, "only unknown outcomes can be acknowledged")
		}
		r.State = "acknowledged"
		if err := tx.Put(ctx, "plugins.http-calls", key, r); err != nil {
			return err
		}
		return s.audit(ctx, p, "http.acknowledge", c.PluginID)
	})
}
