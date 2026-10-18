package auth

import (
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
	"testing"
	"time"
)

func TestDefaultSessionIdleIsSevenDaysWithThirtyDayLifetime(t *testing.T) {
	defaults := config.Defaults(t.TempDir())
	if defaults.Auth.SessionIdle.Value() != 7*24*time.Hour || defaults.Auth.SessionLifetime.Value() != 30*24*time.Hour {
		t.Fatal("unexpected session defaults")
	}
	t.Run("idle", func(t *testing.T) {
		f := fixture(t)
		_, issued := initialized(t, f)
		f.Advance(6 * 24 * time.Hour)
		_, err := f.Service.Authenticate(t.Context(), Authentication{Session: issued.Token})
		testkit.RequireNoError(t, err)
		f.Advance(7 * 24 * time.Hour)
		_, err = f.Service.Authenticate(t.Context(), Authentication{Session: issued.Token})
		testkit.RequireCode(t, err, core.Unauthenticated)
	})
	t.Run("absolute", func(t *testing.T) {
		f := fixture(t)
		_, issued := initialized(t, f)
		for i := 0; i < 4; i++ {
			f.Advance(6 * 24 * time.Hour)
			_, err := f.Service.Authenticate(t.Context(), Authentication{Session: issued.Token})
			testkit.RequireNoError(t, err)
		}
		f.Advance(6 * 24 * time.Hour)
		_, err := f.Service.Authenticate(t.Context(), Authentication{Session: issued.Token})
		testkit.RequireCode(t, err, core.Unauthenticated)
	})
}

func TestMFAKeepsOriginalRememberMeChoice(t *testing.T) {
	for _, remember := range []bool{false, true} {
		name := "session"
		if remember {
			name = "persistent"
		}
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			principal, _ := initialized(t, f)
			enrollment, err := f.Service.BeginMFAEnrollment(t.Context(), principal, verify(t, f, principal, "mfa.enroll"))
			testkit.RequireNoError(t, err)
			codes, err := f.Service.ConfirmMFAEnrollment(t.Context(), principal, ConfirmMFARequest{EnrollmentID: enrollment.ID, Code: f.Code(enrollment)})
			testkit.RequireNoError(t, err)
			login, err := f.Service.Login(t.Context(), LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!"), RememberMe: remember})
			testkit.RequireNoError(t, err)
			issued, err := f.Service.CompleteMFA(t.Context(), MFAChallengeResponse{Challenge: login.Challenge.ID, Code: codes.Codes[0]})
			testkit.RequireNoError(t, err)
			if issued.RememberMe != remember {
				t.Fatal("MFA changed cookie persistence")
			}
		})
	}
}
