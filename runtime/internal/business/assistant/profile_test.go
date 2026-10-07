package assistant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestAvatarImageCropAndLimits(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 600, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 600; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= 200 && x < 400 {
				c = color.RGBA{G: 255, A: 255}
			}
			src.Set(x, y, c)
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, src); err != nil {
		t.Fatal(err)
	}
	avatar, err := AvatarFromImage(append(raw.Bytes(), []byte("metadata-sentinel")...))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := base64.StdEncoding.DecodeString(strings.Split(avatar, ",")[1])
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "jpeg" || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 || len(avatar) > 128<<10 || bytes.Contains(data, []byte("metadata-sentinel")) {
		t.Fatalf("invalid thumbnail: %v", err)
	}
	r, g, b, _ := img.At(5, 128).RGBA()
	if r > 1000 || g < 64000 || b > 1000 {
		t.Fatal("image was not center cropped")
	}
	// Reject oversized declared dimensions before allocating a decoded image.
	huge := append([]byte{}, raw.Bytes()...)
	binary.BigEndian.PutUint32(huge[16:20], 100_000)
	binary.BigEndian.PutUint32(huge[20:24], 100_000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	for _, bad := range [][]byte{nil, []byte("<svg/>"), []byte("not an image"), make([]byte, AvatarImageLimit+1), huge} {
		if _, err = AvatarFromImage(bad); core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("accepted invalid image: %v", err)
		}
	}
	// A 1×1 lossless WebP, decoded without requiring an external codec process.
	webp, _ := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4TBUAAAAvAAAAAAfQ//73v/+BiOh/AAA=")
	if _, err = AvatarFromImage(webp); err != nil {
		t.Fatalf("webp: %v", err)
	}
}

func TestProfileMutationAtomicNoopAndIdentity(t *testing.T) {
	f := newContractFixture(t)
	a, err := f.Service.Hire(t.Context(), owner(), HireRequest{Mutation: mutation("hire", 0), Name: "Before", ModelID: "model", Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	if err != nil {
		t.Fatal(err)
	}
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: a.ID, Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: a.ID, OperationID: "op"}
	name, avatar := "  小满  ", "preset:cat"
	req := SelfProfileRequest{Mutation: mutation("change", a.Version), Name: &name, Avatar: &avatar}
	out, err := f.Service.UpdateSelfProfile(t.Context(), e, req)
	if err != nil || !out.Changed || out.Profile.Name != "小满" || out.Profile.Avatar.ID != "cat" {
		t.Fatalf("update: %+v %v", out, err)
	}
	again, err := f.Service.UpdateSelfProfile(t.Context(), e, req)
	if err != nil || again != out {
		t.Fatalf("idempotency: %+v %v", again, err)
	}
	got, _ := f.Service.Get(t.Context(), owner(), a.ID)
	if got.ModelID != a.ModelID || got.Budget != a.Budget || got.ConversationID != a.ConversationID || got.ID != a.ID {
		t.Fatal("unrelated settings changed")
	}
	req.Mutation = mutation("noop", got.Version)
	noop, err := f.Service.UpdateSelfProfile(t.Context(), e, req)
	if err != nil || noop.Changed || noop.Profile.Version != got.Version {
		t.Fatalf("noop: %+v %v", noop, err)
	}
	avatar = "preset:invalid"
	name = "Must not commit"
	req.Mutation = mutation("invalid", got.Version)
	if _, err = f.Service.UpdateSelfProfile(t.Context(), e, req); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	after, _ := f.Service.Get(t.Context(), owner(), a.ID)
	if after != got {
		t.Fatal("partial update persisted")
	}
	avatar = "preset:fox"
	req.Mutation = mutation("rollback", got.Version)
	f.FailCommitOnce()
	if _, err = f.Service.UpdateSelfProfile(t.Context(), e, req); err == nil {
		t.Fatal("commit failure ignored")
	}
	after, _ = f.Service.Get(t.Context(), owner(), a.ID)
	if after != got {
		t.Fatal("failed commit persisted")
	}
	e.AssistantID = "other"
	req.Mutation = mutation("wrong", got.Version)
	if _, err = f.Service.UpdateSelfProfile(t.Context(), e, req); core.CodeOf(err) != core.Forbidden {
		t.Fatal("identity mismatch accepted", err)
	}
	for _, bad := range []string{"", "  ", "bad\nname", strings.Repeat("满", 257)} {
		if _, err = NormalizeName(bad); err == nil {
			t.Fatalf("name accepted: %q", bad)
		}
	}
}
