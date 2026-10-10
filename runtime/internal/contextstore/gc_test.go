package contextstore

import (
	"context"
	"testing"
	"time"

	"molr.ai/runtime/internal/persistence"
)

func TestReleaseKindScopesReferencesAndBackgroundCollection(t *testing.T) {
	s := fixture(t)
	target := ref("owner", "drop")
	others := []Reference{ref("other-owner", "keep"), ref("owner", "other-assistant"), ref("owner", "other-kind")}
	others[1].AssistantID = "another"
	others[2].Kind = "retained"
	save(t, s, target, doc("discard this distinct content"))
	for _, r := range others {
		save(t, s, r, doc("retained content"))
	}
	before := total(t, s, objects)
	write(t, s, func(ctx context.Context, tx persistence.Tx) error {
		return ReleaseKind(ctx, tx, target.OwnerID, target.AssistantID, target.Kind)
	})
	if n := total(t, s, references); n != int64(len(others)) {
		t.Fatal("release escaped its owner, assistant or kind", n)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { RunGC(ctx, s); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for total(t, s, objects) == before {
		if time.Now().After(deadline) {
			t.Fatal("background collection did not reclaim released content")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, r := range others {
		_ = load(t, s, r)
	}
}
