package debugtrace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func completed(t *testing.T, s *Service, p core.Principal, now *time.Time, text string) core.ID {
	t.Helper()
	*now = now.Add(time.Second)
	ctx := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "execution", "user_message", 1, nil)
	s.Record(ctx, "model.complete", "succeeded", "operation", map[string]string{"Text": text})
	s.Finish(ctx, "succeeded", "")
	ref, _ := ctx.Value(contextKey{}).(*reference)
	if ref == nil {
		t.Fatal("missing trace")
	}
	return ref.id
}
func TestRetentionProtectsHistoryAcrossAgeCapacityAndAllReaders(t *testing.T) {
	s, p, now := fixture(t)
	s.options.MinRuns = 2
	s.options.MaxRuns = 4
	s.options.MaxBytes = 10000
	first := completed(t, s, p, now, strings.Repeat("a", 1000))
	second := completed(t, s, p, now, strings.Repeat("b", 1000))
	third := completed(t, s, p, now, strings.Repeat("c", 1000))
	*now = now.AddDate(0, 0, 30)
	// Old protected data remains readable even before a new write triggers cleanup.
	if _, err := s.Get(t.Context(), p, first); core.CodeOf(err) != core.NotFound {
		t.Fatal("unprotected expired history visible", err)
	}
	for _, id := range []core.ID{second, third} {
		if payload, err := s.Detail(t.Context(), p, id, 2); err != nil || len(payload.Data) == 0 {
			t.Fatal("protected detail hidden", err)
		}
	}
	page, err := s.Trajectory(t.Context(), p, TrajectoryQuery{})
	if err != nil || len(page.Runs) != 2 {
		t.Fatal("trajectory hid protected history", page, err)
	}
	list, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(list.Items) != 2 {
		t.Fatal("runs hid protected history", list, err)
	}
	// Lower the allowance below protected bytes. Metadata continues, full payloads
	// already retained stay intact, and a live run never evicts the completed floor.
	s.options.MaxBytes = 1
	active := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "live", "user_message", 1, nil)
	s.Record(active, "model.start", "started", "operation", map[string]string{"Text": "new detail"})
	for _, id := range []core.ID{second, third} {
		if _, err := s.Detail(t.Context(), p, id, 2); err != nil {
			t.Fatal("new execution evicted protected history", err)
		}
	}
	ref := active.Value(contextKey{}).(*reference)
	payload, err := s.Detail(t.Context(), p, ref.id, 2)
	if err != nil || payload.Status != "capacity_reached" || (len(payload.Data) != 0 && string(payload.Data) != "null") {
		t.Fatal("capacity not explicit", payload, err)
	}
	if _, err := persistence.Load[Run](t.Context(), s.store, "debug.runs", persistence.Key(p.OwnerID, first)); !persistence.Missing(err) {
		t.Fatal("expired unprotected run not cleaned")
	}
	*now = now.AddDate(0, 0, 30)
	next := s.Begin(t.Context(), p.OwnerID, "assistant", "event", "another-live", "user_message", 1, nil)
	if _, err := s.Get(t.Context(), p, ref.id); err != nil {
		t.Fatal("expired active run removed", err)
	}
	s.Finish(next, "succeeded", "")
	s.Finish(active, "succeeded", "")
	rows, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(rows.Items) < 2 {
		t.Fatal("cleanup erased recent executions", rows, err)
	}
}
func TestRetentionCountUsesExactLimitAndAccountScope(t *testing.T) {
	s, p, now := fixture(t)
	s.options.MinRuns = 2
	var ids []core.ID
	for i := 0; i < 6; i++ {
		ids = append(ids, completed(t, s, p, now, fmt.Sprint(i)))
	}
	rows, err := s.Runs(t.Context(), p, Query{})
	if err != nil || len(rows.Items) != 3 {
		t.Fatal("off-by-one count retention", rows, err)
	}
	if rows.Items[2].ID != ids[3] {
		t.Fatal("did not remove oldest first")
	}
	// Another owner never affects the first owner's count/size budget.
	foreign := p
	foreign.OwnerID = "other"
	foreign.SubjectID = "other"
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.settings", "other", map[string]bool{"WorkbenchVisible": true})
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		completed(t, s, foreign, now, "other")
	}
	rows, err = s.Runs(t.Context(), p, Query{})
	if err != nil || len(rows.Items) != 3 || rows.Items[2].ID != ids[3] {
		t.Fatal("cross-owner eviction", rows, err)
	}
}
func TestRetentionAdministrationPersistenceConflictsAndValidation(t *testing.T) {
	s, p, _ := fixture(t)
	deny := func(context.Context, core.Principal) error { return core.NewError(core.Forbidden, "admin required") }
	s.options.Administrator = deny
	q := RetentionRequest{Policy: RetentionOverrides{RetentionDays: intPointer(14), MaxRuns: intPointer(200), MaxBytes: intPointer(128 << 20), MinRuns: intPointer(10)}, Mutation: core.Mutation{IdempotencyKey: "retention-save", ExpectedVersion: 1}}
	if _, err := s.SetRetention(t.Context(), p, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("member could edit policy", err)
	}
	s.options.Administrator = func(context.Context, core.Principal) error { return nil }
	saved, err := s.SetRetention(t.Context(), p, q)
	if err != nil || saved.Version != 2 {
		t.Fatal(saved, err)
	}
	// Reopen the database as on a process restart, independent of defaults.
	db := s.store.(*sqlitestore.Store)
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	s.store = reopened
	restarted := New(s.store, s.options)
	got, err := restarted.Retention(t.Context(), p)
	if err != nil || got.Policy != saved {
		t.Fatal("settings did not persist", got, err)
	}
	replay, err := s.SetRetention(t.Context(), p, q)
	if err != nil || replay != saved {
		t.Fatal("idempotent replay failed", replay, err)
	}
	q.Policy.MinRuns = intPointer(*q.Policy.MinRuns + 1)
	if _, err := s.SetRetention(t.Context(), p, q); core.CodeOf(err) != core.Conflict {
		t.Fatal("changed idempotent payload accepted", err)
	}
	q.Mutation.IdempotencyKey = "stale"
	if _, err := s.SetRetention(t.Context(), p, q); core.CodeOf(err) != core.Conflict {
		t.Fatal("stale version accepted", err)
	}
	q.Mutation.ExpectedVersion = 2
	for _, change := range []func(*RetentionOverrides){func(p *RetentionOverrides) { p.MinRuns = intPointer(0) }, func(p *RetentionOverrides) { p.MinRuns = intPointer(*p.MaxRuns + 1) }, func(p *RetentionOverrides) { p.RetentionDays = intPointer(0) }, func(p *RetentionOverrides) { p.MaxBytes = intPointer(1024) }, func(p *RetentionOverrides) { p.MaxRuns = intPointer(10001) }} {
		bad := q
		change(&bad.Policy)
		if _, err := s.SetRetention(t.Context(), p, bad); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal("bad policy accepted", bad, err)
		}
	}
	// Reading a policy does not expose any other owner's executions or payloads.
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "OwnerID") {
		t.Fatal("private trace leaked")
	}
}

func intPointer(n int) *int { return &n }

func TestUnsetRetentionFieldsStayUnsetAndFollowDefaults(t *testing.T) {
	s, p, _ := fixture(t)
	s.options.MaxRuns = 1000
	s.options.MinRuns = 10
	s.options.Administrator = func(context.Context, core.Principal) error { return nil }
	initial, err := s.AdminRetention(t.Context(), p)
	if err != nil || initial.Configured.MaxRuns != nil || initial.Defaults.MaxRuns != 1000 || initial.MaxRuns != 1000 {
		t.Fatal(initial, err)
	}
	q := RetentionRequest{Policy: RetentionOverrides{MaxRuns: intPointer(2)}, Mutation: core.Mutation{IdempotencyKey: "partial", ExpectedVersion: 1}}
	effective, err := s.SetRetention(t.Context(), p, q)
	if err != nil || effective.MaxRuns != 2 || effective.MinRuns != 2 || effective.RetentionDays != 7 {
		t.Fatal(effective, err)
	}
	stored, err := s.AdminRetention(t.Context(), p)
	if err != nil || stored.Configured.RetentionDays != nil || stored.Configured.MinRuns != nil || stored.Configured.MaxRuns == nil || *stored.Configured.MaxRuns != 2 {
		t.Fatal(stored, err)
	}
	q = RetentionRequest{Mutation: core.Mutation{IdempotencyKey: "clear", ExpectedVersion: 2}}
	effective, err = s.SetRetention(t.Context(), p, q)
	if err != nil || effective.MaxRuns != 1000 || effective.MinRuns != 10 {
		t.Fatal(effective, err)
	}
	// After recreating the service, no explicit numbers should reappear.
	restarted := New(s.store, s.options)
	stored, err = restarted.AdminRetention(t.Context(), p)
	if err != nil || stored.Configured != (RetentionOverrides{}) {
		t.Fatal("empty fields became explicit defaults", stored, err)
	}
	// A deployment default change affects only the unset fields.
	restarted.options.MaxRuns = 800
	stored, err = restarted.AdminRetention(t.Context(), p)
	if err != nil || stored.MaxRuns != 800 || stored.Configured.MaxRuns != nil {
		t.Fatal(stored, err)
	}
	// Existing flat records from the previous version remain explicit overrides.
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "debug.retention", "policy", Retention{Version: 4, RetentionDays: 14, MaxRuns: 200, MaxBytes: 128 << 20, MinRuns: 10})
	}); err != nil {
		t.Fatal(err)
	}
	stored, err = restarted.AdminRetention(t.Context(), p)
	if err != nil || stored.MaxRuns != 200 || stored.Configured.MaxRuns == nil || *stored.Configured.MaxRuns != 200 {
		t.Fatal("lost existing explicit value", stored, err)
	}
}

func TestTrajectoryCleanupRecordsActualPolicyAndAssistantScope(t *testing.T) {
	s, p, now := fixture(t)
	s.options.MinRuns = 1
	s.options.MaxRuns = 2
	query := TrajectoryQuery{Query: Query{AssistantID: "assistant"}}
	read := func(service *Service, principal core.Principal, q TrajectoryQuery) *CleanupEvent {
		t.Helper()
		page, err := service.Trajectory(t.Context(), principal, q)
		if err != nil {
			t.Fatal(err)
		}
		return page.Cleanup
	}
	completed(t, s, p, now, "first")
	completed(t, s, p, now, "second")
	if event := read(s, p, query); event != nil {
		t.Fatal("policy alone created a cleanup event", event)
	}
	// Cleaning this assistant from a different assistant's write must be attributed
	// to the history removed, not the assistant that triggered the shared limit.
	*now = now.Add(time.Second)
	ctx := s.Begin(t.Context(), p.OwnerID, "untouched", "event", "execution", "user_message", 1, nil)
	s.Finish(ctx, "succeeded", "")
	event := read(s, p, query)
	if event == nil || !event.At.Equal(*now) || event.Policy.MaxRuns != 2 {
		t.Fatal("missing actual cleanup policy", event)
	}
	if other := read(s, p, TrajectoryQuery{Query: Query{AssistantID: "untouched"}}); other != nil {
		t.Fatal("cleanup leaked to untouched assistant", other)
	}
	foreign := p
	foreign.OwnerID = "other"
	foreign.SubjectID = "other"
	if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.settings", "other", map[string]bool{"WorkbenchVisible": true})
	}); err != nil {
		t.Fatal(err)
	}
	if other := read(s, foreign, query); other != nil {
		t.Fatal("cleanup leaked across owners", other)
	}
	s.options.MaxRuns = 50
	restarted := New(s.store, s.options)
	if saved := read(restarted, p, query); saved == nil || *saved != *event {
		t.Fatal("cleanup policy was replaced by current settings or lost after restart", saved)
	}
	completed(t, restarted, p, now, "new history without cleanup")
	if saved := read(restarted, p, query); saved == nil || *saved != *event {
		t.Fatal("ordinary writes replaced cleanup", saved)
	}
}
