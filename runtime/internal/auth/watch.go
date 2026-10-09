package auth

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func principalSignal(owner core.ID) persistence.SignalKey {
	return persistence.SignalKey{Namespace: "auth.principal", Key: string(owner)}
}

// PrincipalWatch observes committed account changes and credential revocations.
// Activity timestamps do not notify: Check rereads them at the previous deadline.
// Register before checking validity so a concurrent revocation cannot be lost.
type PrincipalWatch struct {
	persistence.Watch
	manager   *Manager
	principal core.Principal
}

func (m *Manager) WatchPrincipal(p core.Principal) (*PrincipalWatch, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	watch, err := m.store.Watch(principalSignal(p.OwnerID))
	if err != nil {
		return nil, err
	}
	return &PrincipalWatch{Watch: watch, manager: m, principal: p}, nil
}

// Check validates current persisted identity and returns time until its next expiry.
// It never refreshes activity or caches authorization across transactions.
func (w *PrincipalWatch) Check(ctx context.Context) (time.Duration, error) {
	return w.manager.principalValidity(ctx, w.principal)
}
