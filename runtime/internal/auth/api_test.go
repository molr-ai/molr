package auth

import (
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestAuthenticationRejectsAmbiguousCredentials(t *testing.T) {
	for _, a := range []Authentication{{}, {Session: core.NewSecret("s"), Bearer: core.NewSecret("t")}} {
		if core.CodeOf(a.Validate()) != core.Unauthenticated {
			t.Fatal("ambiguous authentication accepted")
		}
	}
	if err := (Authentication{Bearer: core.NewSecret("token")}).Validate(); err != nil {
		t.Fatal(err)
	}
}
