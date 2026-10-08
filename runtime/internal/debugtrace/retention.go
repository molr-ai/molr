package debugtrace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Retention is installation-wide, with independent usage for each owner.
// MinRuns protects completed executions; in-flight executions are always protected.
type Retention struct {
	Version       core.Version
	RetentionDays int
	MaxRuns       int
	MaxBytes      int
	MinRuns       int
}

// Nil is an unset override, not an explicit copy of the deployment default.
type RetentionOverrides struct {
	RetentionDays *int
	MaxRuns       *int
	MaxBytes      *int
	MinRuns       *int
}
type storedRetention struct {
	Version core.Version
	RetentionOverrides
}
type RetentionSettings struct {
	Retention
	Configured RetentionOverrides
	Defaults   Retention
}
type RetentionRequest struct {
	Policy   RetentionOverrides
	Mutation core.Mutation
}

// CleanupEvent retains only the latest actual cleanup and its policy per assistant.
type CleanupEvent struct {
	At     time.Time
	Policy Retention
}

type RetentionStatus struct {
	Policy          Retention
	Runs            int
	Bytes           int
	ProtectedRuns   int
	CapacityReached bool
}

func (s *Service) resolveRetention(row storedRetention) RetentionSettings {
	defaults := Retention{Version: row.Version, RetentionDays: s.options.RetentionDays, MaxRuns: s.options.MaxRuns, MaxBytes: s.options.MaxBytes, MinRuns: s.options.MinRuns}
	effective := defaults
	if row.RetentionDays != nil {
		effective.RetentionDays = *row.RetentionDays
	}
	if row.MaxRuns != nil {
		effective.MaxRuns = *row.MaxRuns
	}
	if row.MaxBytes != nil {
		effective.MaxBytes = *row.MaxBytes
	}
	// An unset minimum follows the default, capped by the effective count limit.
	effective.MinRuns = min(defaults.MinRuns, effective.MaxRuns)
	if row.MinRuns != nil {
		effective.MinRuns = *row.MinRuns
	}
	return RetentionSettings{Retention: effective, Configured: row.RetentionOverrides, Defaults: defaults}
}
func (s *Service) retentionSettings(ctx context.Context, store persistence.Store) (RetentionSettings, error) {
	row, err := persistence.Load[storedRetention](ctx, store, "debug.retention", "policy")
	if persistence.Missing(err) {
		row = storedRetention{Version: 1}
		err = nil
	}
	if err != nil {
		return RetentionSettings{}, err
	}
	return s.resolveRetention(row), nil
}
func (s *Service) retention(ctx context.Context, store persistence.Store) (Retention, error) {
	settings, err := s.retentionSettings(ctx, store)
	return settings.Retention, err
}
func (s *Service) Retention(ctx context.Context, p core.Principal) (RetentionStatus, error) {
	if p.RequireOwner() != nil || s.options.Authorize == nil {
		return RetentionStatus{}, core.NewError(core.Forbidden, "owner debug access required")
	}
	if err := s.options.Authorize(ctx, p); err != nil {
		return RetentionStatus{}, err
	}
	var out RetentionStatus
	err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		policy, err := s.retention(ctx, tx.Store())
		if err != nil {
			return err
		}
		out.Policy = policy
		all, err := persistence.Select[Run](ctx, tx.Store(), "debug.summaries", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID)}})
		if err != nil {
			return err
		}
		runs := ownerRuns(all, p.OwnerID)
		out.ProtectedRuns = len(protectedRuns(runs, policy.MinRuns))
		for _, r := range runs {
			out.Runs++
			out.Bytes += r.Bytes
		}
		out.CapacityReached = out.Bytes >= policy.MaxBytes
		return nil
	})
	return out, err
}
func (s *Service) AdminRetention(ctx context.Context, p core.Principal) (RetentionSettings, error) {
	if s.options.Administrator == nil {
		return RetentionSettings{}, core.NewError(core.Forbidden, "administrator required")
	}
	if err := s.options.Administrator(ctx, p); err != nil {
		return RetentionSettings{}, err
	}
	return s.retentionSettings(ctx, s.store)
}
func (s *Service) SetRetention(ctx context.Context, p core.Principal, q RetentionRequest) (Retention, error) {
	if _, err := s.AdminRetention(ctx, p); err != nil {
		return Retention{}, err
	}
	if err := q.Mutation.Validate(true); err != nil {
		return Retention{}, err
	}
	v := s.resolveRetention(storedRetention{RetentionOverrides: q.Policy}).Retention
	if v.RetentionDays < 1 || v.RetentionDays > 365 || v.MaxRuns < 1 || v.MaxRuns > 10000 || v.MaxBytes < 1<<20 || int64(v.MaxBytes) > 10<<30 || v.MinRuns < 1 || v.MinRuns > v.MaxRuns {
		return Retention{}, core.Invalid("retention", "days 1–365, runs 1–10000, bytes 1 MiB–10 GiB, protected runs 1–max runs required")
	}
	raw, _ := json.Marshal(q)
	hash := sha256.Sum256(raw)
	type receipt struct {
		Hash   [32]byte
		Policy Retention
	}
	var out Retention
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(p.OwnerID, core.ID(q.Mutation.IdempotencyKey))
		var prior receipt
		err := tx.Get(ctx, "debug.retention.mutations", key, &prior)
		if err == nil {
			if prior.Hash != hash {
				return core.NewError(core.Conflict, "idempotency key reused")
			}
			out = prior.Policy
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		old, err := s.retention(ctx, tx.Store())
		if err != nil {
			return err
		}
		if old.Version != q.Mutation.ExpectedVersion {
			return core.NewError(core.Conflict, "retention policy changed")
		}
		v.Version = old.Version + 1
		if err := tx.Put(ctx, "debug.retention", "policy", storedRetention{Version: v.Version, RetentionOverrides: q.Policy}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "debug.retention.mutations", key, receipt{hash, v}); err != nil {
			return err
		}
		// Wake all existing workbench clients, without deleting data during a settings save.
		rows, err := tx.Scan(ctx, "debug.revisions")
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := s.bump(ctx, tx, core.ID(row.Key)); err != nil {
				return err
			}
		}
		out = v
		return nil
	})
	return out, err
}
func ownerRuns(all []Run, owner core.ID) []Run {
	runs := []Run{}
	for _, r := range all {
		if r.OwnerID == owner && r.Trigger != "dream_due" {
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].FirstRevision > runs[j].FirstRevision || (runs[i].FirstRevision == runs[j].FirstRevision && runs[i].ID > runs[j].ID)
		}
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	return runs
}
func protectedRuns(runs []Run, min int) map[core.ID]bool {
	out := map[core.ID]bool{}
	n := 0
	for _, r := range runs {
		if r.State == "running" {
			out[r.ID] = true
		} else if n < min {
			out[r.ID] = true
			n++
		}
	}
	return out
}
func visibleRun(r Run, policy Retention, protected map[core.ID]bool, now time.Time) bool {
	return r.Trigger == "dream_due" || protected[r.ID] || !r.StartedAt.Before(now.AddDate(0, 0, -policy.RetentionDays))
}
