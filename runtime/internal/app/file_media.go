package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
	"molr.ai/runtime/internal/policy"
)

const mediaFileLimit = 64 << 20
const mediaPartLimit = 8 << 20
const mediaCacheLimit = 512 << 20

type mediaReadResult struct {
	SandboxID     core.ID
	Path          string       `json:"path"`
	Version       string       `json:"version"`
	MediaType     string       `json:"media_type"`
	SizeBytes     int64        `json:"size_bytes"`
	Width         int          `json:"width,omitempty"`
	Height        int          `json:"height,omitempty"`
	Duration      float64      `json:"duration_seconds,omitempty"`
	ContentLoaded bool         `json:"content_loaded"`
	Reason        string       `json:"reason,omitempty"`
	StartSeconds  float64      `json:"start_seconds,omitempty"`
	EndSeconds    float64      `json:"end_seconds,omitempty"`
	AudioIncluded bool         `json:"audio_included"`
	Truncated     bool         `json:"truncated"`
	Media         []media.Part `json:",omitempty"`
}

func fileVirtualPath(t tools.FileTarget) string {
	if t.ResourceID != "" {
		return "/resources/" + string(t.ResourceID) + "/" + strings.TrimPrefix(t.Path, "/")
	}
	return t.Path
}
func digestBytes(b []byte) string { d := sha256.Sum256(b); return hex.EncodeToString(d[:]) }
func (f *resourceFileTools) mediaRoot(e core.Execution) string {
	return filepath.Join(f.adapters.dataDir, "media", string(e.Principal.OwnerID), string(e.AssistantID))
}
func (f *resourceFileTools) snapshot(ctx context.Context, e core.Execution, data []byte) (string, error) {
	root := f.mediaRoot(e)
	unlock, err := f.lock(ctx, root)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err = os.MkdirAll(root, 0700); err != nil {
		return "", core.NewError(core.Unavailable, "media cache unavailable")
	}
	hash := digestBytes(data)
	if existing, err := openResourceFile(root, hash); err == nil {
		existing.Close()
		now := time.Now()
		_ = os.Chtimes(filepath.Join(root, hash), now, now)
		return hash, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", core.NewError(core.Unavailable, "media cache unavailable")
	}
	type cached struct {
		name string
		size int64
		used time.Time
	}
	var objects []cached
	var size int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		objects = append(objects, cached{entry.Name(), info.Size(), info.ModTime()})
		size += info.Size()
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].used.Before(objects[j].used) })
	for _, object := range objects {
		if size+int64(len(data)) <= mediaCacheLimit && time.Since(object.used) < 7*24*time.Hour {
			break
		}
		if err = os.Remove(filepath.Join(root, object.name)); err != nil {
			return "", core.NewError(core.Unavailable, "media cache cleanup failed")
		}
		size -= object.size
	}
	_, err = replaceResourceFile(ctx, root, hash, string(data), false, "", func() error { return nil })
	return hash, err
}
func (f *resourceFileTools) read(ctx context.Context, e core.Execution, r tools.FileReadRequest) (json.RawMessage, error) {
	if strings.HasPrefix(r.Path, "/plugins/") {
		return f.readPluginFile(ctx, e, r)
	}
	if f.router != nil {
		if out, remote, err := f.router.file(ctx, e, "file_read", r.FileTarget, r, r.ModelID); remote {
			return out, err
		}
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read)
	if err != nil {
		return nil, err
	}
	file, err := openResourceFile(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, core.NewError(core.Unavailable, "file metadata unavailable")
	}
	version, err := resourceFileVersion(file)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	n, err := file.Read(head)
	if err != nil && err != io.EOF {
		return nil, core.NewError(core.Unavailable, "file read failed")
	}
	head = head[:n]
	mime := http.DetectContentType(head)
	// Sniffing must not treat ordinary source code/XML/SVG/empty files as binary.
	isText := len(head) == 0 || strings.HasPrefix(mime, "text/") || mime == "text/xml; charset=utf-8"
	if isText && !r.MetadataOnly {
		if r.Detail != "" || r.StartSeconds != nil || r.EndSeconds != nil || r.MaxFrames != 0 {
			return nil, core.Invalid("options", "media options cannot be used with text")
		}
		return f.readText(ctx, e, r)
	}
	out := mediaReadResult{SandboxID: "local", Path: fileVirtualPath(r.FileTarget), Version: version, MediaType: mime, SizeBytes: stat.Size()}
	finish := func() (json.RawMessage, error) {
		current, err := resourceFileVersion(file)
		if err != nil {
			return nil, err
		}
		if current != version {
			return nil, fileChanged()
		}
		currentRoot, _, err := f.resolve(ctx, e, r.FileTarget, policy.Read)
		if err != nil {
			return nil, err
		}
		if currentRoot != root {
			return nil, fileChanged()
		}
		return json.Marshal(out)
	}
	if r.TextRange || (r.Offset != 0 && r.Offset != 1) || (r.Limit != 0 && r.Limit != 200) {
		return nil, core.Invalid("range", "line ranges require a text file")
	}
	var provider model.Provider
	if f.providerOverride != nil {
		provider = f.providerOverride(ctx)
	} else if r.ModelID != "" {
		provider, err = f.adapters.provider(ctx, e.Principal, r.ModelID)
		if err != nil {
			return nil, err
		}
		err = f.adapters.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: provider.Model.ResourceID}, ResourceID: provider.Model.ResourceID, ModelID: r.ModelID, Action: policy.Execute, Destination: provider.Endpoint})
		if err != nil {
			return nil, err
		}
	}
	imageFile := strings.HasPrefix(mime, "image/")
	avFile := strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/") || mime == "application/ogg"
	if isText {
		out.Reason = "metadata_only"
		return finish()
	}
	if !imageFile && !avFile {
		out.Reason = "unsupported_file_type"
		return finish()
	}
	if stat.Size() > mediaFileLimit {
		out.Reason = "file_exceeds_64_mib_limit"
		return finish()
	}
	if _, err = file.Seek(0, 0); err != nil {
		return nil, err
	}
	if imageFile {
		if r.StartSeconds != nil || r.EndSeconds != nil || r.MaxFrames != 0 {
			return nil, core.Invalid("options", "time ranges and max_frames require audio or video")
		}
		cfg, _, decodeErr := image.DecodeConfig(file)
		if decodeErr == nil {
			out.Width = cfg.Width
			out.Height = cfg.Height
			if cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
				out.Reason = "image_dimensions_exceed_limit"
				return finish()
			}
		}
		if r.MetadataOnly {
			out.Reason = "metadata_only"
			return finish()
		}
		if !provider.Model.SupportsImages {
			out.Reason = "model_does_not_support_image"
			return finish()
		}
		if decodeErr != nil && mime != "image/webp" {
			out.Reason = "unsupported_or_invalid_image"
			return finish()
		}
		if stat.Size() > mediaPartLimit {
			out.Reason = "image_exceeds_8_mib_limit"
			return finish()
		}
		file.Seek(0, 0)
		data, err := io.ReadAll(io.LimitReader(file, mediaPartLimit+1))
		if err != nil {
			return nil, err
		}
		if len(data) > mediaPartLimit {
			out.Reason = "image_exceeds_8_mib_limit"
			return finish()
		}
		if mime == "image/webp" {
			data, err = normalizeWebP(ctx, data)
			if err != nil || len(data) > mediaPartLimit {
				out.Reason = "media_decoder_unavailable_or_invalid_file"
				return finish()
			}
			mime = "image/png"
			cfg, _, err = image.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				out.Reason = "invalid_image"
				return finish()
			}
			out.Width = cfg.Width
			out.Height = cfg.Height
		}
		// Decode fully to reject malformed payloads; dimensions were bounded first.
		if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
			out.Reason = "invalid_image"
			return finish()
		}
		hash, err := f.snapshot(ctx, e, data)
		if err != nil {
			return nil, err
		}
		out.Media = []media.Part{{FullAccess: r.FullAccess, Kind: "image", MIME: mime, Snapshot: hash, Scope: digestBytes([]byte(root)), ResourceID: r.ResourceID, Path: r.Path, Version: version, Detail: r.Detail}}
		out.ContentLoaded = true
		return finish()
	}
	return f.readAV(ctx, e, r, file, root, &out, provider, finish)
}

// Resolve durable snapshots just before encoding. Removed grants, changed
// bindings, deleted files and model changes never replay inaccessible media.
func (f *resourceFileTools) prepareMedia(ctx context.Context, req model.Request, provider model.Provider) ([]model.Input, error) {
	inputs := append([]model.Input(nil), req.Inputs...)
	total, count := 0, 0
	for i := len(inputs) - 1; i >= 0; i-- {
		inputs[i].Media = append([]media.Part(nil), inputs[i].Media...)
		for j := range inputs[i].Media {
			p := &inputs[i].Media[j]
			p.Data = nil
			if p.Kind == "image" && !provider.Model.SupportsImages || p.Kind == "audio" && (!provider.Model.SupportsAudio || provider.Protocol == model.DeepSeekChat) {
				continue
			}
			if f.router != nil && p.SandboxID != "" && p.SandboxID != "local" {
				data, _, err := f.router.media(ctx, req.Execution, *p)
				if err != nil {
					if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound || core.CodeOf(err) == core.Unavailable {
						continue
					}
					return nil, err
				}
				if len(data) > mediaPartLimit || total+len(data) > 24<<20 || count >= 32 {
					continue
				}
				total += len(data)
				count++
				p.Data = data
				continue
			}
			root, name, err := f.resolve(ctx, req.Execution, tools.FileTarget{FullAccess: p.FullAccess, SandboxID: "local", ResourceID: p.ResourceID, Path: p.Path}, policy.Read)
			if err != nil {
				if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound {
					continue
				}
				return nil, err
			}
			if p.Scope != digestBytes([]byte(root)) {
				continue
			}
			source, err := openResourceFile(root, name)
			if err != nil {
				continue
			}
			source.Close()
			if len(p.Snapshot) != 64 {
				return nil, core.Invalid("media", "invalid snapshot")
			}
			if _, err = hex.DecodeString(p.Snapshot); err != nil {
				return nil, core.Invalid("media", "invalid snapshot")
			}
			file, err := openResourceFile(f.mediaRoot(req.Execution), p.Snapshot)
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(file, mediaPartLimit+1))
			file.Close()
			if err != nil {
				return nil, core.NewError(core.Unavailable, "media cache read failed")
			}
			if len(data) > mediaPartLimit || digestBytes(data) != p.Snapshot {
				return nil, core.NewError(core.FailedPrecondition, "media snapshot is invalid")
			}
			if total+len(data) > 24<<20 || count >= 32 {
				continue
			}
			total += len(data)
			count++
			now := time.Now()
			_ = os.Chtimes(filepath.Join(f.mediaRoot(req.Execution), p.Snapshot), now, now)
			p.Data = data
		}
	}
	return inputs, nil
}
