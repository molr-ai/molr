package auth

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
	"testing"
)

func TestReauthenticationDefaultsOffAndProtectsPolicyChanges(t *testing.T) {
	f := fixture(t)
	p, _ := initialized(t, f)
	ctx := testkit.Context(t)
	status, err := f.Service.SecurityStatus(ctx, p)
	testkit.RequireNoError(t, err)
	if status.ReauthenticationEnabled || len(status.UnprotectedPurposes) != 3 {
		t.Fatal("sensitive action verification must default off")
	}
	for _, purpose := range []string{"credential.put", "credential.delete", "export"} {
		testkit.RequireNoError(t, f.Service.CheckVerification(ctx, p, Verification{}, purpose))
		if f.Service.CheckVerification(ctx, core.Principal{}, Verification{}, purpose) == nil {
			t.Fatal("anonymous operation bypassed authentication")
		}
	}
	for _, purpose := range []string{"security.update", "password.change", "mfa.enroll", "mfa.disable", "token.create", "cleanup", "unknown"} {
		if f.Service.CheckVerification(ctx, p, Verification{}, purpose) == nil {
			t.Fatalf("account security verification bypassed: %s", purpose)
		}
	}
	_, err = f.Service.SetReauthentication(ctx, p, ReauthenticationSettings{Enabled: true, ExpectedVersion: status.Version})
	if err == nil {
		t.Fatal("policy changed without verification")
	}
	status, err = f.Service.SetReauthentication(ctx, p, ReauthenticationSettings{Enabled: true, ExpectedVersion: status.Version, Verification: verify(t, f, p, "security.update")})
	testkit.RequireNoError(t, err)
	if !status.ReauthenticationEnabled || len(status.UnprotectedPurposes) != 0 {
		t.Fatal("policy was not enabled")
	}
	for _, purpose := range []string{"credential.put", "credential.delete", "export"} {
		if f.Service.CheckVerification(ctx, p, Verification{}, purpose) == nil {
			t.Fatalf("enabled verification bypassed: %s", purpose)
		}
	}
	stored, err := f.Repository.Account(ctx)
	testkit.RequireNoError(t, err)
	if !stored.Account.ReauthenticationEnabled {
		t.Fatal("verification policy was not persisted")
	}
	proof := verify(t, f, p, "security.update")
	_, err = f.Service.SetReauthentication(ctx, p, ReauthenticationSettings{ExpectedVersion: status.Version - 1, Verification: proof})
	testkit.RequireCode(t, err, core.Conflict)
	status, err = f.Service.SetReauthentication(ctx, p, ReauthenticationSettings{ExpectedVersion: status.Version, Verification: proof})
	testkit.RequireNoError(t, err)
	if status.ReauthenticationEnabled {
		t.Fatal("verification policy was not disabled")
	}
	testkit.RequireNoError(t, f.Service.CheckVerification(ctx, p, Verification{}, "credential.put"))
}
