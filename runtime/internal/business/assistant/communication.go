package assistant

import (
	"context"
	"molr.ai/runtime/internal/core"
	"slices"
	"time"
)

type PeerCommunicationAuthorization struct {
	Enabled       bool
	AllAssistants bool
	AssistantIDs  []core.ID
	ExpiresAt     *time.Time
	EnabledAt     *time.Time `json:",omitempty"`
}

type PeerCommunicationRequest struct {
	Mutation      core.Mutation
	AssistantID   core.ID
	Authorization PeerCommunicationAuthorization
}

func (s *service) SetPeerCommunication(ctx context.Context, p core.Principal, r PeerCommunicationRequest) (Assistant, error) {
	return mutate(ctx, s, p, "peer-communication", r.AssistantID, r.Mutation, r, true, true, func(ctx context.Context) (Assistant, error) {
		a, err := s.repo.Get(ctx, p.OwnerID, r.AssistantID)
		if err != nil {
			return a, err
		}
		if a.Version != r.Mutation.ExpectedVersion {
			return a, conflict()
		}
		if a.EmployerID != "" || a.State != Active {
			return a, forbidden()
		}
		h := r.Authorization
		if len(h.AssistantIDs) > 200 || (h.Enabled && !h.AllAssistants && len(h.AssistantIDs) == 0) || (h.Enabled && h.ExpiresAt != nil && !h.ExpiresAt.After(s.now())) {
			return a, invalid("authorization")
		}
		h.AssistantIDs = slices.Clone(h.AssistantIDs)
		slices.Sort(h.AssistantIDs)
		if len(slices.Compact(slices.Clone(h.AssistantIDs))) != len(h.AssistantIDs) {
			return a, invalid("assistant_ids")
		}
		if h.AllAssistants {
			h.AssistantIDs = nil
		}
		for _, id := range h.AssistantIDs {
			if !h.Enabled {
				break
			}
			peer, err := s.repo.Get(ctx, p.OwnerID, id)
			if err != nil {
				return a, err
			}
			if peer.ID == a.ID || peer.EmployerID != "" || peer.State != Active {
				return a, invalid("assistant_ids")
			}
		}
		h.EnabledAt = nil
		if old := a.PeerCommunication; old != nil {
			h.EnabledAt = old.EnabledAt
			sameExpiry := h.ExpiresAt == nil && old.ExpiresAt == nil || h.ExpiresAt != nil && old.ExpiresAt != nil && h.ExpiresAt.Equal(*old.ExpiresAt)
			if old.Enabled == h.Enabled && old.AllAssistants == h.AllAssistants && slices.Equal(old.AssistantIDs, h.AssistantIDs) && sameExpiry {
				return a, nil
			}
		}
		if h.Enabled {
			at := s.now()
			h.EnabledAt = &at
		}
		a.PeerCommunication = &h
		old := a.Version
		a.Version++
		a.UpdatedAt = s.now()
		if err := s.repo.Save(ctx, a, old); err != nil {
			return a, err
		}
		return a, s.profileChanged(ctx, a)
	})
}

// CanContact uses the intersection of current permissions for both peers.
// Employer/worker communication uses its independent live hiring allowance.
func CanContact(a, b Assistant, now time.Time) bool {
	if a.ID == b.ID || a.OwnerID != b.OwnerID || a.State != Active || b.State != Active {
		return false
	}
	if a.EmployerID == "" && b.EmployerID == "" {
		return peerAllows(a, b.ID, now) && peerAllows(b, a.ID, now)
	}
	worker, employer := a, b
	if worker.EmployerID == "" {
		worker, employer = b, a
	}
	if worker.EmployerID != employer.ID || employer.EmployerID != "" {
		return false
	}
	h := employer.Hiring
	return h != nil && h.Enabled && h.ID == worker.HiringID && (worker.ExpiresAt == nil || worker.ExpiresAt.After(now)) && (h.ExpiresAt == nil || h.ExpiresAt.After(now))
}
func peerAllows(a Assistant, id core.ID, now time.Time) bool {
	h := a.PeerCommunication
	return h != nil && h.Enabled && (h.ExpiresAt == nil || h.ExpiresAt.After(now)) && (h.AllAssistants || slices.Contains(h.AssistantIDs, id))
}
