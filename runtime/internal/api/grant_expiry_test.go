package api

import (
	"molr.ai/runtime/internal/policy"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFullAccessWireExpiryMustBeExplicit(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{"Scope":{"FullAccess":true}}`, false},
		{`{"Scope":{"FullAccess":true},"ExpiresAt":null}`, true},
		{`{"Scope":{"FullAccess":true,"Once":true},"ExpiresAt":"2030-01-01T00:00:00Z"}`, true},
		{`{"Scope":{}}`, true},
		{`{"Scope":{"FullAccess":true},"ExpiresAt":null,"unexpected":true}`, false},
	} {
		r := httptest.NewRequest("POST", "/api/grants", strings.NewReader(tc.raw))
		_, err := decode[policy.GrantRequest](r)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.raw, err)
		}
	}
	for _, raw := range []string{`{"Decision":"approve","Scope":{"FullAccess":true}}`, `{"Decision":"approve","Additional":[{"Scope":{"FullAccess":true}}]}`} {
		if _, err := decode[policy.PermissionResolution](httptest.NewRequest("POST", "/api/permission-requests/resolve", strings.NewReader(raw))); err == nil {
			t.Fatal("approval defaulted to permanent", raw)
		}
	}
}
