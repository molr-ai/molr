package assistant

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/persistence"
	"testing"
	"time"
)

func TestStateSignalOnlyCommittedLifecycleChanges(t *testing.T) {
	f := newContractFixture(t)
	s := f.Service.(*service)
	a := hire(t, f)
	w, err := s.store.Watch(StateSignal(a.OwnerID, a.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	other, err := s.store.Watch(StateSignal(a.OwnerID, "other"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	save := func(ctx context.Context) error { v := a; v.Version++; return s.repo.Save(ctx, v, a.Version) }
	if err = save(t.Context()); err != nil {
		t.Fatal(err)
	}
	a.Version++
	select {
	case <-w.Changes():
		t.Fatal("unchanged state notified")
	default:
	}
	a.State = Dismissed
	rollback := errors.New("rollback")
	err = s.store.Write(t.Context(), func(ctx context.Context, _ persistence.Tx) error {
		if err := save(ctx); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-w.Changes():
		t.Fatal("rollback notified")
	default:
	}
	if err = save(t.Context()); err != nil {
		t.Fatal(err)
	}
	a.Version++
	select {
	case <-w.Changes():
	case <-time.After(time.Second):
		t.Fatal("stop did not notify")
	}
	select {
	case <-other.Changes():
		t.Fatal("another assistant notified")
	default:
	}
	a.State = Active
	if err = save(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Changes():
	case <-time.After(time.Second):
		t.Fatal("resume did not notify")
	}
}
