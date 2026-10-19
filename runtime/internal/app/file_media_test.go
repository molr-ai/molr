package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/modelcatalog"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"testing"
)

func mediaTestModel(t *testing.T, f resourceAccessFixture) model.Provider {
	t.Helper()
	binding := resource.Binding{Endpoint: "https://provider.example/v1/chat/completions", AllowedOrigins: []string{"https://provider.example"}, Models: []resource.ModelConfig{{Name: "vision", SupportsTools: true, SupportsImages: true, SupportsAudio: true, ContextLimit: 32768, Pricing: &resource.ModelPricing{Currency: "USD"}}}}
	r, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "media-provider"}, Kind: resource.Model, Provider: "openai-compatible", Name: "media", Binding: binding})
	need(t, err)
	entry := modelcatalog.Entries(r, binding)[0]
	f.grant(t, r.ID, "media-model", []policy.Action{policy.Read, policy.Execute}, policy.Scope{ModelID: entry.ID}, nil)
	return f.ad.modelCatalog().Describe(r, binding, entry)
}
func testPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 12, 8))
	im.Set(0, 0, c)
	var b bytes.Buffer
	need(t, png.Encode(&b, im))
	return b.Bytes()
}
func TestFileMediaMetadataSnapshotsModelSwitchAndRevocation(t *testing.T) {
	f, fs, target := fileFixture(t)
	p := mediaTestModel(t, f)
	original := testPNG(t, color.White)
	filename := filepath.Join(f.root, "write/data")
	need(t, os.WriteFile(filename, original, 0600))
	for _, metadata := range []bool{true, false} {
		raw, err := fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, MetadataOnly: metadata})
		need(t, err)
		var out mediaReadResult
		need(t, json.Unmarshal(raw, &out))
		if out.ContentLoaded || out.Width != 12 || out.Height != 8 || len(out.Media) != 0 {
			t.Fatalf("text model got content: %s", raw)
		}
	}
	raw, err := fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, ModelID: p.Model.ID})
	need(t, err)
	var out mediaReadResult
	need(t, json.Unmarshal(raw, &out))
	if !out.ContentLoaded || len(out.Media) != 1 {
		t.Fatalf("missing image: %s", raw)
	}
	if bytes.Contains(raw, []byte("iVBOR")) {
		t.Fatal("base64 in tool result")
	}
	// Replacing the source must not silently change an already read image.
	need(t, os.WriteFile(filename, testPNG(t, color.Black), 0600))
	req := model.Request{Execution: f.execution, Inputs: []model.Input{{Role: model.Tool, ReplyTo: "read", Media: out.Media}}}
	inputs, err := fs.prepareMedia(t.Context(), req, p)
	need(t, err)
	if !bytes.Equal(inputs[0].Media[0].Data, original) {
		t.Fatal("snapshot did not survive file overwrite")
	}
	p.Model.SupportsImages = false
	inputs, err = fs.prepareMedia(t.Context(), req, p)
	need(t, err)
	if len(inputs[0].Media[0].Data) != 0 {
		t.Fatal("model switch leaked media")
	}
	p.Model.SupportsImages = true
	grants, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{AssistantID: f.execution.AssistantID})
	need(t, err)
	for _, g := range grants.Items {
		if g.ResourceID == f.storage.ID {
			err = f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: g.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-media", ExpectedVersion: g.Version}})
			need(t, err)
		}
	}
	inputs, err = fs.prepareMedia(t.Context(), req, p)
	need(t, err)
	if len(inputs[0].Media[0].Data) != 0 {
		t.Fatal("revocation leaked cached media")
	}
}
func TestFileMediaOptionsAndMalformedImages(t *testing.T) {
	f, fs, target := fileFixture(t)
	p := mediaTestModel(t, f)
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), testPNG(t, color.White)[:24], 0600))
	raw, err := fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, ModelID: p.Model.ID})
	need(t, err)
	var out mediaReadResult
	need(t, json.Unmarshal(raw, &out))
	if out.ContentLoaded {
		t.Fatal("malformed image loaded")
	}
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), []byte("plain text"), 0600))
	if _, err = fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, Detail: "high"}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("media options accepted for text")
	}
	raw, err = fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, MetadataOnly: true})
	need(t, err)
	if bytes.Contains(raw, []byte("plain text")) {
		t.Fatal("metadata-only read text")
	}
}

func TestFileReadUnknownBinaryReturnsMetadata(t *testing.T) {
	f, fs, target := fileFixture(t)
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), []byte{0, 1, 2}, 0600))
	raw, err := fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target})
	need(t, err)
	var out mediaReadResult
	need(t, json.Unmarshal(raw, &out))
	if out.ContentLoaded || out.SizeBytes != 3 || out.Reason != "unsupported_file_type" {
		t.Fatalf("unexpected binary result: %s", raw)
	}
}

func TestMediaWindowKeepsRecentReadsAndSurvivesCacheEviction(t *testing.T) {
	f, fs, target := fileFixture(t)
	p := mediaTestModel(t, f)
	need(t, os.WriteFile(filepath.Join(f.root, "write/data"), testPNG(t, color.White), 0600))
	raw, err := fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: target, ModelID: p.Model.ID})
	need(t, err)
	var out mediaReadResult
	need(t, json.Unmarshal(raw, &out))
	inputs := make([]model.Input, 33)
	for i := range inputs {
		inputs[i] = model.Input{Role: model.Tool, ReplyTo: "read", Media: out.Media}
	}
	req := model.Request{Execution: f.execution, Inputs: inputs}
	loaded, err := fs.prepareMedia(t.Context(), req, p)
	need(t, err)
	if len(loaded[0].Media[0].Data) != 0 || len(loaded[32].Media[0].Data) == 0 {
		t.Fatal("media window did not retain recent reads")
	}
	need(t, os.Remove(filepath.Join(fs.mediaRoot(f.execution), out.Media[0].Snapshot)))
	loaded, err = fs.prepareMedia(t.Context(), req, p)
	need(t, err)
	for _, input := range loaded {
		if len(input.Media[0].Data) != 0 {
			t.Fatal("missing snapshot replayed")
		}
	}
}
