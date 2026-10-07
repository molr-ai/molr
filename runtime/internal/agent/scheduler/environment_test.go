package scheduler

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnavailableEnvironmentDoesNotClaimOrConsumeAttempts(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var ready atomic.Bool
	probed := make(chan struct{}, 1)
	handled := make(chan Claim, 1)
	svc := NewService(store, Options{Authorizer: fixtureAuthorizer{}, Concurrency: 1, Ready: func(context.Context) bool {
		select {
		case probed <- struct{}{}:
		default:
		}
		return ready.Load()
	}, Handle: func(_ context.Context, c Claim) error { handled <- c; return nil }})
	owner := core.Principal{Kind: core.ActorOwner, OwnerID: "owner", SubjectID: "owner", SessionID: "session"}
	err = event.NewWriter(store).AppendAssistant(t.Context(), event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", Kind: event.ResumeRequested, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	select {
	case <-probed:
	case <-time.After(time.Second):
		t.Fatal("missing availability check")
	}
	page, err := svc.Jobs(t.Context(), owner, Query{AssistantID: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Attempts != 0 || page.Items[0].State != Queued {
		t.Fatal(page)
	}
	select {
	case <-handled:
		t.Fatal("offline job was executed")
	default:
	}
	ready.Store(true)
	select {
	case c := <-handled:
		if c.Job.Attempts != 1 {
			t.Fatal(c.Job)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("environment recovery did not resume")
	}
	cancel()
	<-done
}
