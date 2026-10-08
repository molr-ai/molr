package app

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type contextGrant struct {
	permissionGrant
	ResourceID core.ID
	Name       string
	Kind       resource.Kind
	Version    core.Version
}

// Authorization is refreshed at each model boundary without probing sandboxes.
// Expiration changes the snapshot even when no explicit mutation occurs.
func permissionsContext(ctx context.Context, store persistence.Store, p core.Principal, now time.Time) (agent.ContextDocument, error) {
	var document agent.ContextDocument
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		grants, err := persistence.Select[policy.Grant](ctx, store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("RevokedAt", nil), persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", p.SubjectID)}, Any: [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(now)}}}})
		if err != nil {
			return err
		}
		rows := []contextGrant{}
		for _, grant := range grants {
			if grant.OwnerID != p.OwnerID || grant.AssistantID != p.SubjectID || grant.RevokedAt != nil || grant.ExpiresAt != nil && !grant.ExpiresAt.After(now) {
				continue
			}
			r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, grant.ResourceID)
			if persistence.Missing(err) {
				continue
			}
			if err != nil {
				return err
			}
			rows = append(rows, contextGrant{permissionGrant: permissionGrant{grant.Version, grant.Constraints, grant.Temporary, grant.ID, grant.Actions, grant.Scope, grant.ExpiresAt}, ResourceID: r.ID, Name: r.Name, Kind: r.Kind, Version: grant.Version})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		revision := permissionDigest(rows)
		out := struct {
			Granted   []contextGrant
			Total     int
			Truncated bool
			Revision  string
		}{Granted: []contextGrant{}, Total: len(rows), Revision: revision}
		bytes := 0
		for _, row := range rows {
			raw, err := json.Marshal(row)
			if err != nil {
				return err
			}
			if len(out.Granted) >= 200 || bytes+len(raw) > 128<<10 {
				out.Truncated = true
				break
			}
			out.Granted = append(out.Granted, row)
			bytes += len(raw)
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		document = agent.ContextDocument{Kind: "permissions", Source: "permissions", Scope: "assistant", Revision: revision, Content: string(raw)}
		return nil
	})
	return document, err
}
