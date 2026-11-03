package assistant

import (
	"molr.ai/runtime/internal/core"
	"testing"
	"time"
)

func TestCanContactIntersectsScopeExpiryAndEmployment(t *testing.T) {
	now := time.Now()
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	allow := func() *PeerCommunicationAuthorization {
		return &PeerCommunicationAuthorization{Enabled: true, AllAssistants: true}
	}
	tests := []struct {
		name   string
		change func(*Assistant, *Assistant)
		want   bool
	}{
		{"all", func(a, b *Assistant) {}, true},
		{"selected-both", func(a, b *Assistant) {
			a.PeerCommunication.AllAssistants = false
			a.PeerCommunication.AssistantIDs = []core.ID{b.ID}
			b.PeerCommunication.AllAssistants = false
			b.PeerCommunication.AssistantIDs = []core.ID{a.ID}
		}, true},
		{"sender-scope", func(a, b *Assistant) {
			a.PeerCommunication.AllAssistants = false
			a.PeerCommunication.AssistantIDs = []core.ID{"other"}
		}, false},
		{"receiver-scope", func(a, b *Assistant) {
			b.PeerCommunication.AllAssistants = false
			b.PeerCommunication.AssistantIDs = []core.ID{"other"}
		}, false},
		{"sender-expiry", func(a, b *Assistant) { a.PeerCommunication.ExpiresAt = &past }, false},
		{"receiver-boundary", func(a, b *Assistant) { b.PeerCommunication.ExpiresAt = &now }, false},
		{"future", func(a, b *Assistant) {
			a.PeerCommunication.ExpiresAt = &future
			b.PeerCommunication.ExpiresAt = &future
		}, true},
		{"disabled", func(a, b *Assistant) { a.PeerCommunication.Enabled = false }, false},
		{"unconfigured", func(a, b *Assistant) { b.PeerCommunication = nil }, false},
		{"different-owner", func(a, b *Assistant) { b.OwnerID = "another-owner" }, false},
		{"dismissed", func(a, b *Assistant) { b.State = Dismissed }, false},
		{"self", func(a, b *Assistant) { b.ID = a.ID }, false},
		{"worker-exception", func(a, b *Assistant) {
			a.PeerCommunication = nil
			b.PeerCommunication = nil
			a.Hiring = &HiringAuthorization{ID: "h", Enabled: true}
			b.EmployerID = a.ID
			b.HiringID = "h"
		}, true},
		{"worker-revoked", func(a, b *Assistant) {
			a.Hiring = &HiringAuthorization{ID: "h", Enabled: false}
			b.EmployerID = a.ID
			b.HiringID = "h"
		}, false},
		{"worker-expired", func(a, b *Assistant) {
			a.Hiring = &HiringAuthorization{ID: "h", Enabled: true, ExpiresAt: &past}
			b.EmployerID = a.ID
			b.HiringID = "h"
		}, false},
		{"another-worker", func(a, b *Assistant) { b.EmployerID = "another-employer" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Assistant{ID: "a", OwnerID: "owner", State: Active, PeerCommunication: allow()}
			b := Assistant{ID: "b", OwnerID: "owner", State: Active, PeerCommunication: allow()}
			tc.change(&a, &b)
			if got := CanContact(a, b, now); got != tc.want {
				t.Fatalf("CanContact=%v, want %v", got, tc.want)
			}
			if got := CanContact(b, a, now); got != tc.want {
				t.Fatalf("reverse CanContact=%v, want %v", got, tc.want)
			}
		})
	}
}
