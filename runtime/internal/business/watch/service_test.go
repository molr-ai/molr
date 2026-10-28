package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"path/filepath"
	"testing"
	"time"
)

type allow struct{}

func (allow) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}
func fixture(t *testing.T) (*Service, core.Principal, Watch) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db, Options{Authorizer: allow{}})
	p := core.Principal{Kind: core.ActorOwner, OwnerID: "owner", SubjectID: "owner", SessionID: "session"}
	w, err := s.Save(t.Context(), p, Request{Mutation: core.Mutation{IdempotencyKey: "create"}, AssistantID: "assistant", Title: "Price watch", Instructions: "Tell me about changes", Watcher: Config{ComputeID: "compute", Script: "exec watcher"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, p, w
}
func begin(t *testing.T, s *Service, p core.Principal, w Watch) Watch {
	t.Helper()
	w, err := s.Begin(t.Context(), p.OwnerID, w.ID, w.Version)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func receive(t *testing.T, s *Service, p core.Principal, w Watch, line string) {
	t.Helper()
	ack, err := s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(line))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if json.Unmarshal(ack, &v) != nil || v["type"] != "ack" {
		t.Fatal(string(ack))
	}
}
func TestWatcherProtocolStateAndIdempotentEvents(t *testing.T) {
	s, p, w := fixture(t)
	w = begin(t, s, p, w)
	if _, err := s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"first","data":1}`)); err == nil {
		t.Fatal("event before ready accepted")
	}
	receive(t, s, p, w, `{"type":"ready","version":1}`)
	receive(t, s, p, w, `{"type":"checkpoint","state":{"price":599}}`)
	line := `{"type":"event","key":"drop-1","data":{"price":499},"state":{"price":499}}`
	receive(t, s, p, w, line)
	receive(t, s, p, w, `{"type":"checkpoint","state":{"price":399}}`)
	receive(t, s, p, w, line)
	got, err := s.Get(t.Context(), p, w.ID)
	if err != nil || len(got.Pending) != 1 || string(got.Checkpoint) != `{"price":399}` || got.LastHealthyAt == nil {
		t.Fatalf("state/event lost: %+v %v", got, err)
	}
	if _, err = s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"drop-1","data":{"price":1}}`)); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
	receive(t, s, p, w, `{"type":"complete","state":{"done":true}}`)
	got, _ = s.Get(t.Context(), p, w.ID)
	if got.State != "completed" || len(got.Pending) != 1 {
		t.Fatal("complete discarded pending event")
	}
	if _, err = s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(line)); err == nil {
		t.Fatal("late event accepted")
	}
}

type rollbackStore struct{ persistence.Store }

func (s rollbackStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		return errors.New("injected commit failure")
	})
}
func TestWatcherEventCheckpointRollBackTogether(t *testing.T) {
	s, p, w := fixture(t)
	w = begin(t, s, p, w)
	receive(t, s, p, w, `{"type":"ready","version":1}`)
	original := s.store
	s.store = rollbackStore{original}
	if _, err := s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"one","data":1,"state":2}`)); err == nil {
		t.Fatal("injected failure ignored")
	}
	s.store = original
	got, _ := s.Get(t.Context(), p, w.ID)
	if len(got.Pending) != 0 || string(got.Checkpoint) != "null" {
		t.Fatal("partial commit")
	}
	receive(t, s, p, w, `{"type":"event","key":"one","data":1,"state":2}`)
	got, _ = s.Get(t.Context(), p, w.ID)
	if len(got.Pending) != 1 || string(got.Checkpoint) != "2" {
		t.Fatal("dedup survived rollback")
	}
}
func TestWatcherRejectsMalformedAndBoundsQueue(t *testing.T) {
	s, p, w := fixture(t)
	w = begin(t, s, p, w)
	for _, line := range []string{"log output", `{}`, `null`, `{"type":"ready","version":2}`, `{"type":"ready","version":1,"unknown":1}`, `{"type":"ready","version":1} {}`, `{"type":"checkpoint"}`, `{"type":"event","key":"x"}`, `{"type":"complete","data":1}`} {
		if _, err := s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(line)); err == nil {
			t.Fatalf("accepted %s", line)
		}
	}
	receive(t, s, p, w, `{"type":"ready","version":1}`)
	for _, line := range []string{`{"Type":"event","key":"x","data":1}`, `{"type":"event","type":"event","key":"x","data":1}`, `{"type":"event","key":"x","data":1,"version":0}`, `{"type":"complete","key":""}`, "{\"type\":\"event\",\"key\":\"x\",\"data\":\"\xff\"}"} {
		if _, err := s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(line)); err == nil {
			t.Fatalf("accepted invalid envelope %q", line)
		}
	}
	for i := 0; i < MaxPending; i++ {
		receive(t, s, p, w, fmt.Sprintf(`{"type":"event","key":"%d","data":%d,"state":%d}`, i, i, i))
	}
	if _, err := s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"overflow","data":1,"state":999}`)); core.ReasonOf(err) != "watch_queue_full" {
		t.Fatal(err)
	}
	got, _ := s.Get(t.Context(), p, w.ID)
	if len(got.Pending) != MaxPending || len(got.Recent) != 100 || string(got.Checkpoint) != "127" {
		t.Fatal("queue bound did not preserve state")
	}
}
func TestWatcherPauseRevisionExpiryAndFailureEpisode(t *testing.T) {
	s, p, w := fixture(t)
	w = begin(t, s, p, w)
	receive(t, s, p, w, `{"type":"ready","version":1}`)
	notify, err := s.End(t.Context(), p.OwnerID, w.ID, w.RunID, "authorization_unavailable", false)
	if err != nil || !notify {
		t.Fatal(notify, err)
	}
	if again, err := s.End(t.Context(), p.OwnerID, w.ID, w.RunID, "authorization_unavailable", false); err != nil || again {
		t.Fatal("duplicate failure", err)
	}
	w2, err := s.Save(t.Context(), p, Request{ID: w.ID, Mutation: core.Mutation{IdempotencyKey: "pause", ExpectedVersion: w.Version}, AssistantID: w.AssistantID, Title: w.Title, Instructions: w.Instructions, Watcher: w.Watcher, Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Begin(t.Context(), p.OwnerID, w.ID, w2.Version); err == nil {
		t.Fatal("paused watcher started")
	}
	if _, err = s.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(`{"type":"checkpoint","state":1}`)); err == nil {
		t.Fatal("stale execution accepted")
	}
	expiry := time.Now().Add(-time.Second)
	w3, err := s.Save(t.Context(), p, Request{ID: w.ID, Mutation: core.Mutation{IdempotencyKey: "expire", ExpectedVersion: w2.Version}, AssistantID: w.AssistantID, Title: w.Title, Instructions: w.Instructions, Watcher: w.Watcher, EndAt: &expiry})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Begin(t.Context(), p.OwnerID, w.ID, w3.Version); err == nil {
		t.Fatal("expired watcher started")
	}
	foreign := p
	foreign.OwnerID = "another"
	if _, err = s.Get(t.Context(), foreign, w.ID); err == nil {
		t.Fatal("cross owner access")
	}
	if err = s.Delete(t.Context(), p, w.ID, core.Mutation{IdempotencyKey: "delete", ExpectedVersion: w3.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(t.Context(), s.store, p.OwnerID, w.ID); !persistence.Missing(err) {
		t.Fatal("delete failed", err)
	}
}
