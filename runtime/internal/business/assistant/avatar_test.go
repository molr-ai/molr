package assistant

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	avatarutil "molr.ai/runtime/internal/avatar"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func avatarPNG(width int) string {
	var out bytes.Buffer
	_ = png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, 32)))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}
func TestAvatarValidation(t *testing.T) {
	for _, value := range []string{"", "preset:cat", avatarPNG(32)} {
		if _, err := avatarutil.Normalize(value); err != nil {
			t.Fatalf("valid avatar rejected: %v", err)
		}
	}
	for _, value := range []string{"preset:unknown", "https://example.org/avatar.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,AAAA", avatarPNG(257), strings.Repeat("a", 128<<10+1), strings.Replace(avatarPNG(32), "image/png", "image/jpeg", 1)} {
		if _, err := avatarutil.Normalize(value); err == nil {
			t.Fatal("invalid avatar accepted")
		}
	}
	// Strip appended content rather than persisting it as part of the identity.
	original := avatarPNG(32)
	data, _ := base64.StdEncoding.DecodeString(strings.Split(original, ",")[1])
	cleaned, err := avatarutil.Normalize("data:image/png;base64," + base64.StdEncoding.EncodeToString(append(data, []byte("private metadata")...)))
	if err != nil || cleaned != original {
		t.Fatalf("metadata not stripped: %v", err)
	}
}
func TestAvatarLifecycle(t *testing.T) {
	f := testkit.RequireFactory(t, "assistant", newContractFixture)
	ctx := testkit.Context(t)
	budget := core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}
	a, err := f.Service.Hire(ctx, owner(), HireRequest{Mutation: mutation("avatar-hire", 0), Name: "Cat", ModelID: "model", Budget: budget, Avatar: "preset:cat"})
	if err != nil || a.Avatar != "preset:cat" {
		t.Fatalf("hire: %+v %v", a, err)
	}
	update := func(key string, value *string) (Assistant, error) {
		return f.Service.Update(ctx, owner(), UpdateRequest{Mutation: mutation(key, a.Version), ID: a.ID, Name: a.Name, ModelID: a.ModelID, Budget: a.Budget, Avatar: value})
	}
	a, err = update("omit", nil)
	if err != nil || a.Avatar != "preset:cat" {
		t.Fatalf("omitted field lost avatar: %v", err)
	}
	uploaded := avatarPNG(32)
	a, err = update("upload", &uploaded)
	if err != nil || a.Avatar != uploaded {
		t.Fatalf("upload: %v", err)
	}
	got, err := f.Service.Get(ctx, owner(), a.ID)
	if err != nil || got.Avatar != uploaded {
		t.Fatalf("persistence: %v", err)
	}
	clone, err := f.Service.Clone(ctx, owner(), CloneRequest{Mutation: mutation("clone-avatar", 0), SourceID: a.ID, Name: "Copy"})
	if err != nil || clone.Avatar != a.Avatar {
		t.Fatalf("clone: %v", err)
	}
	_, err = f.Service.Update(ctx, otherOwner(), UpdateRequest{Mutation: mutation("other-avatar", a.Version), ID: a.ID, Name: a.Name, ModelID: a.ModelID, Budget: a.Budget, Avatar: &uploaded})
	wantDenied(t, err)
	bad := "preset:missing"
	_, err = update("bad", &bad)
	wantCode(t, err, core.InvalidArgument)
	empty := ""
	a, err = update("clear", &empty)
	if err != nil || a.Avatar != "" {
		t.Fatalf("clear: %v", err)
	}
}
