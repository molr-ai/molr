package auth

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestProfileAvatarLifecycleAndIsolation(t *testing.T) {
	f := fixture(t)
	admin, _ := initialized(t, f)
	m := f.Service.(*Manager)
	ctx := t.Context()
	beforeAdmin, err := m.CurrentUser(ctx, admin)
	testkit.RequireNoError(t, err)
	member, err := m.CreateUser(ctx, admin, CreateUserRequest{Username: "avatar-member", Password: core.NewSecret("avatar-member-password-2026"), GroupID: DefaultGroupID, Mutation: core.Mutation{IdempotencyKey: "avatar-member"}})
	testkit.RequireNoError(t, err)
	login, err := m.Login(ctx, LoginRequest{Username: member.Username, Password: core.NewSecret("avatar-member-password-2026")})
	testkit.RequireNoError(t, err)
	p, err := m.Authenticate(ctx, Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	avatar := "preset:cat"
	profile, err := m.UpdateProfile(ctx, p, "My avatar", &avatar, member.Version)
	testkit.RequireNoError(t, err)
	if profile.Avatar != avatar || profile.DisplayName != "My avatar" {
		t.Fatalf("profile not saved: %+v", profile)
	}
	profile, err = m.UpdateProfile(ctx, p, "Name only", nil, profile.Version)
	testkit.RequireNoError(t, err)
	if profile.Avatar != avatar {
		t.Fatal("name update cleared avatar")
	}

	for _, invalid := range []string{"preset:missing", "https://example.com/avatar.png", "data:image/svg+xml;base64,PHN2Zy8+", "data:image/png;base64,AAAA", strings.Repeat("a", 128<<10+1)} {
		_, err = m.UpdateProfile(ctx, p, "Must not save", &invalid, profile.Version)
		testkit.RequireCode(t, err, core.InvalidArgument)
	}
	saved, err := m.CurrentUser(ctx, p)
	testkit.RequireNoError(t, err)
	if saved.Avatar != avatar || saved.DisplayName != profile.DisplayName || saved.Version != profile.Version {
		t.Fatal("invalid update changed account")
	}

	var raw bytes.Buffer
	testkit.RequireNoError(t, png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	clean := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw.Bytes())
	uploaded := "data:image/png;base64," + base64.StdEncoding.EncodeToString(append(raw.Bytes(), []byte("private image metadata")...))
	profile, err = m.UpdateProfile(ctx, p, profile.DisplayName, &uploaded, profile.Version)
	testkit.RequireNoError(t, err)
	if profile.Avatar != clean {
		t.Fatal("thumbnail was not sanitized")
	}
	// Reading through a fresh authenticated session returns the persisted avatar.
	again, err := m.Authenticate(ctx, Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	saved, err = m.CurrentUser(ctx, again)
	testkit.RequireNoError(t, err)
	if saved.Avatar != clean {
		t.Fatal("avatar not persisted")
	}
	empty := ""
	_, err = m.UpdateProfile(ctx, p, "Stale name", &empty, member.Version)
	testkit.RequireCode(t, err, core.Conflict)
	saved, err = m.CurrentUser(ctx, p)
	testkit.RequireNoError(t, err)
	if saved.Avatar != clean || saved.DisplayName != profile.DisplayName {
		t.Fatal("conflicting update changed profile")
	}
	afterAdmin, err := m.CurrentUser(ctx, admin)
	testkit.RequireNoError(t, err)
	if afterAdmin.Account != beforeAdmin.Account {
		t.Fatal("member profile changed administrator")
	}
	_, err = m.UpdateProfile(ctx, core.Principal{}, "Anonymous", &avatar, profile.Version)
	testkit.RequireCode(t, err, core.Unauthenticated)
	profile, err = m.UpdateProfile(ctx, p, profile.DisplayName, &empty, profile.Version)
	testkit.RequireNoError(t, err)
	if profile.Avatar != "" {
		t.Fatal("clear did not restore initial avatar")
	}
}
