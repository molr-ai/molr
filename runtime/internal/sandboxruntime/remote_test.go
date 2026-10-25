package sandboxruntime

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"path/filepath"
	"testing"
	"time"
)

type fakeHost struct {
	client fakeClient
	epoch  uint64
	denied bool
	probes int
}
type fakeClient struct {
	execute, await int
	request        sm.Request
	resumed        sm.Identity
	before         func()
}

func (c *fakeClient) Execute(ctx context.Context, q sm.Request, check func(context.Context) error) (json.RawMessage, error) {
	c.execute++
	c.request = q
	if c.before != nil {
		c.before()
	}
	return json.RawMessage(`{"done":true}`), check(ctx)
}
func (c *fakeClient) Await(ctx context.Context, id sm.Identity, check func(context.Context) error) (json.RawMessage, error) {
	c.await++
	c.resumed = id
	return json.RawMessage(`{"done":true}`), check(ctx)
}
func (h *fakeHost) Connect(context.Context, core.Principal, core.ID) (Client, sm.Info, error) {
	return &h.client, sm.Info{UserGroupQuotas: true}, nil
}
func (h *fakeHost) Info(context.Context, core.Principal, core.ID) (sm.Info, error) {
	h.probes++
	return sm.Info{Enabled: true, Storage: true}, nil
}
func (h *fakeHost) Epoch(context.Context, core.ID) (uint64, error) { return h.epoch, nil }
func (h *fakeHost) CheckExecution(context.Context, core.Execution) error {
	if h.denied {
		return denied()
	}
	return nil
}
func (h *fakeHost) CheckConnection(context.Context, core.Principal, core.ID) error {
	if h.denied {
		return denied()
	}
	return nil
}
func (h *fakeHost) CheckMembership(context.Context, core.ID, core.ID) error {
	if h.denied {
		return denied()
	}
	return nil
}
func fixture(t *testing.T) (*Remote, *fakeHost, core.Execution) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHost{epoch: 1}
	s := New(db, h)
	t.Cleanup(func() { s.Close(); db.Close() })
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	return s, h, e
}
func TestRemoteExecutionReusesFrozenIdentityAndRechecksEpoch(t *testing.T) {
	s, h, e := fixture(t)
	ctx := t.Context()
	q := sm.Request{Method: "file_write"}
	check := func(context.Context) error { return nil }
	if _, err := s.Execute(ctx, e, "node", q, check); err != nil {
		t.Fatal(err)
	}
	if h.client.execute != 1 || h.client.request.Identity.ExecutionID != "run" || h.client.request.Epoch != 1 || h.client.request.Deadline.IsZero() {
		t.Fatal(h.client)
	}
	resumed := e
	resumed.Principal.ExecutionID = "new-run"
	if _, err := s.Execute(ctx, resumed, "node", q, check); err != nil {
		t.Fatal(err)
	}
	if h.client.execute != 1 || h.client.await != 1 || h.client.resumed.ExecutionID != "run" {
		t.Fatal("replayed mutation", h.client)
	}
	if _, err := s.Execute(ctx, e, "other-node", q, check); core.CodeOf(err) != core.Forbidden {
		t.Fatal("changed frozen route", err)
	}
	e.OperationID = "epoch-change"
	h.client.before = func() { h.epoch++ }
	if _, err := s.Execute(ctx, e, "node", q, check); core.CodeOf(err) != core.Forbidden {
		t.Fatal("changed authorization epoch accepted", err)
	}
	h.client.before = nil
	h.denied = true
	e.OperationID = "revoked"
	if _, err := s.Execute(ctx, e, "node", q, check); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revoked execution accepted", err)
	}
}
func TestRemoteWaitRecoversOnceAndRespectsRevocation(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume", true: "revoked"}[revoked], func(t *testing.T) {
			s, h, e := fixture(t)
			e.TaskID = "task"
			ctx := t.Context()
			now := time.Now().UTC()
			err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				if err := tx.Put(ctx, "business.assistants", persistence.Key("owner", "assistant"), assistant.Assistant{ID: "assistant", OwnerID: "owner", State: assistant.Active, Version: 1}); err != nil {
					return err
				}
				return task.NewRepository(tx.Store()).Insert(ctx, task.Task{ID: e.TaskID, OwnerID: "owner", AssistantID: "assistant", State: task.Running, Version: 1, CreatedAt: now, UpdatedAt: now})
			})
			if err != nil {
				t.Fatal(err)
			}
			s.Remember(e, "node", core.NewError(core.Unavailable, "offline"))
			rows, err := persistence.Scan[Wait](ctx, s.store, waitBucket)
			if err != nil || len(rows) != 1 {
				t.Fatal(rows, err)
			}
			h.denied = revoked
			s.Tick(ctx, now)
			s.Close()
			rows, err = persistence.Scan[Wait](ctx, s.store, waitBucket)
			if err != nil || len(rows) != 0 || h.probes != 1 {
				t.Fatal(rows, err, h.probes)
			}
			events, err := persistence.Scan[event.AssistantEvent](ctx, s.store, "agent.events")
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, v := range events {
				if v.Kind == event.ResumeRequested && v.TaskID == e.TaskID {
					count++
				}
			}
			if count != map[bool]int{false: 1, true: 0}[revoked] {
				t.Fatal("unexpected wakeups", events)
			}
			s.Tick(ctx, now.Add(time.Minute))
			if h.probes != 1 {
				t.Fatal("closed manager probed")
			}
		})
	}
}
