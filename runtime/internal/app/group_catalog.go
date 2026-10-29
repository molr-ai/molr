package app

import (
	"context"
	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/plugins"
	"molr.ai/runtime/internal/quota"
	"time"
)

func groupCatalog(ctx context.Context, p core.Principal, a adapters, s *sandboxRouter, t tools.Catalog) ([]accountview.CatalogItem, error) {
	out := []accountview.CatalogItem{}
	installed, err := catalog.Select[plugins.Installation](ctx, a.store, "plugins.installations", persistence.Query{})
	if err != nil {
		return nil, err
	}
	for _, v := range installed {
		if !v.Deleted && v.OwnerID == catalog.Scope {
			out = append(out, accountview.CatalogItem{Kind: "plugin", ID: plugins.Key(v.ID), Name: v.Manifest.Name})
		}
	}
	models, err := a.models(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		out = append(out, accountview.CatalogItem{Kind: "model", ID: m.ID, Name: m.Name})
	}
	shared, err := catalog.Select[resource.Resource](ctx, a.store, "business.resources", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", resource.Connection), persistence.Eq("State", resource.Available)}})
	if err != nil {
		return nil, err
	}
	for _, r := range shared {
		if r.Kind == resource.Connection && r.State == resource.Available && r.OwnerID == catalog.Scope {
			out = append(out, accountview.CatalogItem{Kind: "connection", ID: r.ID, Name: r.Name})
		}
	}
	defs, err := t.Catalog(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, d := range defs {
		out = append(out, accountview.CatalogItem{Kind: "tool", ID: core.ID(d.Name), Name: d.Name})
	}
	info := s.localInfo(ctx)
	if info.Enabled {
		limit := info.AuthorizationCeilings
		out = append(out, accountview.CatalogItem{Kind: "sandbox", ID: "local", Name: "Local sandbox", GPUs: info.GPUs, GPUAccessMode: info.Capabilities.GPUAccessMode, Limits: &limit, FullAccessAvailable: info.Capabilities.FullAccess, WorkspaceQuotaSupported: info.WorkspaceProjectQuotas})
	}
	connections, err := catalog.Select[sandboxConnection](ctx, a.store, "sandbox.connections", persistence.Query{})
	if err != nil {
		return nil, err
	}
	for _, c := range connections {
		if c.OwnerID == catalog.Scope && c.View.State != "disconnected" {
			limit := c.View.Info.AuthorizationCeilings
			out = append(out, accountview.CatalogItem{Kind: "sandbox", ID: c.View.ID, Name: c.View.Name, GPUs: c.View.Info.GPUs, GPUAccessMode: c.View.Info.Capabilities.GPUAccessMode, Limits: &limit, FullAccessAvailable: c.View.Info.Capabilities.FullAccess, WorkspaceQuotaSupported: c.View.Info.WorkspaceProjectQuotas})
		}
	}
	return out, nil
}

func accountQuotas(ctx context.Context, store persistence.Store, m *auth.Manager, p core.Principal) (accountview.AccountQuotas, error) {
	out := accountview.AccountQuotas{Resources: []accountview.ResourceUsage{}, AsOf: time.Now().UTC()}
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		u, err := m.CurrentUser(ctx, p)
		if err != nil {
			return err
		}
		out.Group, err = groupPolicy(ctx, tx.Store(), p.OwnerID)
		if err != nil {
			return err
		}
		out.Summary, err = userUsage(ctx, tx.Store(), u)
		if err != nil {
			return err
		}
		out.ContextStorage, err = contextstore.AccountUsage(ctx, tx, p.OwnerID)
		if err != nil {
			return err
		}
		for _, ref := range out.Group.Resources {
			start, end := groupPeriod(ref.Quota, out.AsOf)
			v := accountview.ResourceUsage{Name: string(ref.ID), Kind: ref.Kind, ID: ref.ID, PeriodStart: start, Totals: quota.Totals{Spent: []core.Money{}, Reserved: []core.Money{}}}
			if ref.Kind == "connection" {
				if resource, loadErr := resource.NewRepository(tx.Store()).Get(ctx, p.OwnerID, ref.ID); loadErr == nil {
					v.Name = resource.Name
				} else if !persistence.Missing(loadErr) {
					return loadErr
				}
			}
			if ref.Kind == "sandbox" {
				if ref.ID == "local" {
					v.Name = "Local sandbox"
				} else {
					c, loadErr := catalog.Load[sandboxConnection](ctx, tx.Store(), "sandbox.connections", ref.ID)
					if loadErr == nil {
						v.Name = c.View.Name
					} else if !persistence.Missing(loadErr) {
						return loadErr
					}
				}
			}
			if !end.IsZero() {
				v.PeriodEnd = &end
			}
			switch ref.Kind {
			case "model":
				v.Totals, err = quota.ModelTotals(ctx, tx.Store(), p.OwnerID, ref.ID, start, end)
			case "tool", "connection":
				v.Totals, err = quota.CallTotals(ctx, tx.Store(), ref.Kind, p.OwnerID, ref.ID, "", start, end)
			}
			if err != nil {
				return err
			}
			out.Resources = append(out.Resources, v)
		}
		return nil
	})
	return out, err
}
