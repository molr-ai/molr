package credential

import (
	"context"
	"molr.ai/runtime/internal/core"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOAuthResolutionSerializesRefreshAndPersistsBeforeUse(t *testing.T) {
	f := fixture(t)
	c := put(t, f)
	a := f.Bind(c.ID)
	v := f.Manager.(*Vault)
	var rotations atomic.Int32
	v.options.ResolveProvider = func(ctx context.Context, a Access, m Credential, s core.Secret) (core.Secret, core.Secret, error) {
		if s.Reveal() == "never-export-this-secret" {
			rotations.Add(1)
			return core.NewSecret("rotated-session"), core.NewSecret("bearer"), nil
		}
		if s.Reveal() != "rotated-session" {
			t.Error("wrong persisted session")
		}
		return core.Secret{}, core.NewSecret("bearer"), nil
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			err := v.WithSecret(t.Context(), a, func(secret core.Secret) error {
				if secret.Reveal() != "bearer" {
					t.Error("session material exposed")
				}
				row, err := v.repo.Get(t.Context(), f.Owner.OwnerID, c.ID)
				if err != nil {
					return err
				}
				stored, err := v.decrypt(row)
				if stored.Reveal() != "rotated-session" {
					t.Error("bearer used before refresh persisted")
				}
				return err
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if rotations.Load() != 1 {
		t.Fatal("refresh token raced", rotations.Load())
	}
	f.RevokeAccess(a)
	if err := v.WithSecret(t.Context(), a, func(core.Secret) error { t.Error("revoked credential used"); return nil }); err == nil {
		t.Fatal("revocation ignored")
	}
}
