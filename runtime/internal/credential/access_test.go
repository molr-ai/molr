package credential

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
	"testing"
)

func TestAccessTrustedPrincipalAndOptionalExecution(t *testing.T) {
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	assistant := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}
	execution := core.Execution{Principal: assistant, AssistantID: "assistant", OperationID: "operation"}
	for _, tc := range []struct {
		name   string
		access Access
		want   core.Principal
	}{
		{"owner connection check", Access{Principal: owner}, owner},
		{"assistant execution", Access{Principal: assistant, Execution: execution}, assistant},
		{"legacy execution", Access{Execution: execution}, assistant},
		{"background restricted token", Access{Principal: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, TokenID: "refresh-token"}}, core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, TokenID: "refresh-token"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.access
			a.ResourceID = "resource"
			a.CredentialID = "credential"
			a.Origin = "https://provider.example"
			testkit.RequireNoError(t, a.Validate())
			p, err := a.EffectivePrincipal()
			testkit.RequireNoError(t, err)
			if p != tc.want {
				t.Fatal("principal changed")
			}
		})
	}
	mismatched := assistant
	mismatched.SubjectID = "other-assistant"
	for _, a := range []Access{{}, {Principal: assistant}, {Principal: owner, Execution: execution}, {Principal: mismatched, Execution: execution}, {Principal: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner}}} {
		if _, err := a.EffectivePrincipal(); err == nil {
			t.Fatal("missing or mismatched trusted identity accepted")
		}
	}
}

func TestAccessRequiresExactOriginAndBoundIdentifiers(t *testing.T) {
	a := Access{Principal: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, ResourceID: "resource", CredentialID: "credential", Origin: "https://provider.example"}
	for _, origin := range []string{"", "file:///secret", "https://user:password@provider.example", "https://provider.example/path", "https://provider.example?query=secret", "https://provider.example?", "https://provider.example#fragment"} {
		bad := a
		bad.Origin = origin
		testkit.RequireCode(t, bad.Validate(), core.InvalidArgument)
	}
	for _, mutate := range []func(*Access){func(a *Access) { a.ResourceID = "" }, func(a *Access) { a.CredentialID = "../secret" }} {
		bad := a
		mutate(&bad)
		testkit.RequireCode(t, bad.Validate(), core.InvalidArgument)
	}
}
