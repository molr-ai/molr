package contextstore

import (
	"context"
	"fmt"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func usage(t *testing.T, s persistence.Store, owner core.ID) Usage {
	t.Helper()
	var result Usage
	err := s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var err error
		if owner == "" {
			result, err = GlobalUsage(ctx, tx)
		} else {
			result, err = AccountUsage(ctx, tx, owner)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestUsageDeduplicatesWithinScopeAndTracksCollection(t *testing.T) {
	s := fixture(t)
	if got := usage(t, s, ""); got != (Usage{}) {
		t.Fatal(got)
	}
	a, a2, b, branch := ref("alice", "one"), ref("alice", "two"), ref("bob", "one"), ref("bob", "branch")
	save(t, s, a, doc("shared", "hello"))
	save(t, s, a2, doc("shared", "hello"))
	save(t, s, b, doc("shared", "hello"))
	shared := usage(t, s, "alice")
	if shared.Bytes <= 0 || shared.Messages != 2 || usage(t, s, "bob") != shared || usage(t, s, "") != shared {
		t.Fatal("duplicate refs or accounts inflated storage", shared)
	}
	save(t, s, branch, doc("shared", "hello", "private branch"))
	global, bob := usage(t, s, ""), usage(t, s, "bob")
	if usage(t, s, "alice") != shared || bob.Messages != 3 || global != bob || global.Bytes >= shared.Bytes+bob.Bytes {
		t.Fatal("wrong sharing scope", global, bob, shared)
	}
	write(t, s, func(ctx context.Context, tx persistence.Tx) error {
		for _, r := range []Reference{a, a2, branch} {
			if err := Release(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	if usage(t, s, "alice") != (Usage{}) || usage(t, s, "bob") != shared || usage(t, s, "") != global {
		t.Fatal("live account usage must exclude orphans while global retains them")
	}
	gc(t, s)
	if usage(t, s, "") != shared {
		t.Fatal("collection did not reduce global usage")
	}
}

func TestUsagePaginatesReferencesAndObjects(t *testing.T) {
	s := fixture(t)
	texts := make([]string, 205)
	for i := range texts {
		texts[i] = fmt.Sprintf("message %d", i)
	}
	write(t, s, func(ctx context.Context, tx persistence.Tx) error {
		if err := Save(ctx, tx, ref("alice", "long"), doc(texts...)); err != nil {
			return err
		}
		for i := range 205 {
			if err := Save(ctx, tx, ref("alice", fmt.Sprint(i)), doc(texts[0])); err != nil {
				return err
			}
		}
		return nil
	})
	account, global := usage(t, s, "alice"), usage(t, s, "")
	if account.Messages != 205 || account != global {
		t.Fatal(account, global)
	}
	if usage(t, s, "bob") != (Usage{}) {
		t.Fatal("unrelated account included")
	}
	err := s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error { _, err := AccountUsage(ctx, tx, ""); return err })
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("empty owner accepted", err)
	}
}
