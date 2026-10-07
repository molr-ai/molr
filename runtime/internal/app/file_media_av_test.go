package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMediaCodecIsolationAndAudioVideoRead(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	if _, err := mediaCommand(t.Context(), t.TempDir(), "ffprobe", "-version"); err != nil {
		if os.Getenv("MOLR_REQUIRE_MEDIA_SANDBOX") == "1" {
			t.Fatalf("media isolation unavailable: %v", err)
		}
		t.Skipf("media isolation unavailable: %v", err)
	}
	f, fs, target := fileFixture(t)
	p := mediaTestModel(t, f)
	// Explicit zero audio rate is a free fixture, not missing pricing.
	r, err := f.b.Services.Resources.Get(t.Context(), f.owner, p.Model.ResourceID)
	need(t, err)
	zero := int64(0)
	_, err = f.b.Services.Resources.ConfigureModel(t.Context(), f.owner, resource.ConfigureModelRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "audio-config", ExpectedVersion: r.Version}, Model: resource.ModelConfig{Name: p.Model.Name, SupportsImages: true, SupportsAudio: true, SupportsTools: true, ContextLimit: 32768, Pricing: &resource.ModelPricing{Currency: "USD", AudioInputMicrosPerMillion: &zero}}})
	need(t, err)
	for _, kind := range []string{"audio", "video", "webp"} {
		t.Run(kind, func(t *testing.T) {
			filename := filepath.Join(f.root, "write/data")
			args := []string{"-nostdin", "-v", "error", "-y", "-threads", "1", "-filter_threads", "1"}
			switch kind {
			case "audio":
				args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:a", "pcm_s16le", "-f", "wav", filename)
			case "video":
				args = append(args, "-f", "lavfi", "-i", "color=red:size=64x48:rate=2:duration=2", "-c:v", "mpeg4", "-f", "mp4", filename)
			case "webp":
				args = append(args, "-f", "lavfi", "-i", "color=red:size=64x48", "-frames:v", "1", "-c:v", "libwebp", "-f", "image2", filename)
			}
			if out, err := exec.CommandContext(t.Context(), "ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v %s", err, out)
			}
			request := tools.FileReadRequest{FileTarget: target, ModelID: p.Model.ID}
			if kind == "video" {
				request.MaxFrames = 2
			}
			raw, err := fs.read(t.Context(), f.execution, request)
			need(t, err)
			var out mediaReadResult
			need(t, json.Unmarshal(raw, &out))
			if !out.ContentLoaded {
				t.Fatalf("media not loaded: %s", raw)
			}
			if kind == "video" && (len(out.Media) != 2 || out.Media[0].Timestamp == nil || out.Media[1].Timestamp == nil || *out.Media[1].Timestamp <= *out.Media[0].Timestamp || out.AudioIncluded) {
				t.Fatalf("bad frames: %s", raw)
			}
			if kind == "audio" && (!out.AudioIncluded || out.Media[0].Kind != "audio") {
				t.Fatalf("bad audio: %s", raw)
			}
		})
	}
}
