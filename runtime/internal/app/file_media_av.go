package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
)

type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("output limit")
	}
	return b.Buffer.Write(p)
}

// Fixed, networkless preprocessing. Never fall back to running codecs on the
// host with access to user files, credentials or arbitrary URLs/playlists.
func mediaCommand(ctx context.Context, dir, program string, args ...string) ([]byte, error) {
	for _, name := range []string{"bwrap", "prlimit", program} {
		if _, err := exec.LookPath(name); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	flags := []string{"--unshare-all", "--die-with-parent", "--new-session", "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64", "--ro-bind", "/etc/ld.so.cache", "/etc/ld.so.cache", "--ro-bind", "/etc/alternatives", "/etc/alternatives", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--bind", dir, "/media", "--chdir", "/media", "--clearenv", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "OPENBLAS_NUM_THREADS", "1", "--", "/usr/bin/prlimit", "--as=805306368", "--cpu=15", "--fsize=16777216", "--nofile=64", "--", "/usr/bin/" + program}
	flags = append(flags, args...)
	cmd := exec.CommandContext(ctx, "bwrap", flags...)
	var out = cappedBuffer{max: 1 << 20}
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func (f *resourceFileTools) readAV(ctx context.Context, e core.Execution, r tools.FileReadRequest, file *os.File, root string, out *mediaReadResult, provider model.Provider, finish func() (json.RawMessage, error)) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if r.Detail != "" {
		return nil, core.Invalid("detail", "detail applies only to images")
	}
	dir, err := os.MkdirTemp("", "molr-media-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input, err := os.OpenFile(filepath.Join(dir, "input"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(input, io.LimitReader(file, mediaFileLimit+1))
	closeErr := input.Close()
	if err != nil || closeErr != nil {
		return nil, core.NewError(core.Unavailable, "media staging failed")
	}
	probe, err := mediaCommand(ctx, dir, "ffprobe", "-v", "error", "-protocol_whitelist", "file", "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", "input")
	if err != nil {
		out.Reason = "media_decoder_unavailable_or_invalid_file"
		return finish()
	}
	var info struct {
		Format  struct{ Duration string }
		Streams []struct {
			CodecType     string `json:"codec_type"`
			Width, Height int
		}
	}
	if json.Unmarshal(probe, &info) != nil {
		out.Reason = "invalid_media"
		return finish()
	}
	out.Duration, _ = strconv.ParseFloat(info.Format.Duration, 64)
	video, audio := false, false
	for _, s := range info.Streams {
		if s.CodecType == "video" {
			video = true
			out.Width = s.Width
			out.Height = s.Height
		}
		if s.CodecType == "audio" {
			audio = true
		}
	}
	if r.MetadataOnly {
		out.Reason = "metadata_only"
		return finish()
	}
	if video && !provider.Model.SupportsImages {
		out.Reason = "model_does_not_support_image"
		return finish()
	}
	if !video && (!audio || !provider.Model.SupportsAudio) {
		out.Reason = "model_does_not_support_audio"
		return finish()
	}
	if !video && r.MaxFrames != 0 {
		return nil, core.Invalid("max_frames", "max_frames requires video")
	}
	if !video && (provider.Pricing == nil || provider.Pricing.AudioInputMicrosPerMillion == nil) {
		out.Reason = "audio_pricing_not_configured"
		return finish()
	}
	start := 0.0
	if r.StartSeconds != nil {
		start = *r.StartSeconds
	}
	end := start + 30
	if r.EndSeconds != nil {
		end = *r.EndSeconds
	}
	if end-start > 60 {
		return nil, core.Invalid("end_seconds", "read at most 60 seconds at a time")
	}
	if out.Duration > 0 {
		end = min(end, out.Duration)
	}
	if end <= start {
		return nil, core.Invalid("start_seconds", "range is outside the media duration")
	}
	out.StartSeconds = start
	out.EndSeconds = end
	out.Truncated = out.Duration == 0 || end < out.Duration || start > 0
	base := []string{"-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1", "-protocol_whitelist", "file", "-ss", fmt.Sprint(start), "-i", "input"}
	if video {
		frames := r.MaxFrames
		if frames == 0 {
			frames = 6
		}
		// Individual seeks preserve exact requested timestamps and bound output.
		for i := 0; i < frames; i++ {
			stamp := start + (end-start)*float64(i)/float64(frames)
			args := append([]string(nil), base...)
			args[10] = fmt.Sprint(stamp)
			name := fmt.Sprintf("frame-%02d.jpg", i)
			args = append(args, "-map", "0:v:0", "-an", "-sn", "-dn", "-frames:v", "1", "-vf", "scale=w=1280:h=1280:force_original_aspect_ratio=decrease", "-threads", "1", "-q:v", "3", name)
			if _, err = mediaCommand(ctx, dir, "ffmpeg", args...); err != nil {
				out.Reason = "media_decode_failed"
				out.Media = nil
				return finish()
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				out.Reason = "media_decode_failed"
				out.Media = nil
				return finish()
			}
			if len(data) > mediaPartLimit {
				return nil, core.Invalid("media", "frame exceeds size limit")
			}
			hash, err := f.snapshot(ctx, e, data)
			if err != nil {
				return nil, err
			}
			out.Media = append(out.Media, media.Part{FullAccess: r.FullAccess, Kind: "image", MIME: "image/jpeg", Snapshot: hash, Scope: digestBytes([]byte(root)), ResourceID: r.ResourceID, Path: r.Path, Version: out.Version, Timestamp: &stamp})
		}
		out.Reason = "video_frames_only_no_audio"
	} else {
		args := append(base, "-t", fmt.Sprint(end-start), "-map", "0:a:0", "-vn", "-sn", "-dn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "audio.wav")
		if _, err = mediaCommand(ctx, dir, "ffmpeg", args...); err != nil {
			out.Reason = "media_decode_failed"
			return finish()
		}
		data, err := os.ReadFile(filepath.Join(dir, "audio.wav"))
		if err != nil {
			return nil, err
		}
		if len(data) > mediaPartLimit {
			return nil, core.Invalid("media", "audio exceeds size limit")
		}
		hash, err := f.snapshot(ctx, e, data)
		if err != nil {
			return nil, err
		}
		out.Media = []media.Part{{FullAccess: r.FullAccess, Kind: "audio", MIME: "audio/wav", Snapshot: hash, Scope: digestBytes([]byte(root)), ResourceID: r.ResourceID, Path: r.Path, Version: out.Version}}
		out.AudioIncluded = true
	}
	out.ContentLoaded = len(out.Media) > 0
	// Normalize generic Ogg containers after probing without trusting extensions.
	if strings.Contains(out.MediaType, "ogg") {
		if video {
			out.MediaType = "video/ogg"
		} else {
			out.MediaType = "audio/ogg"
		}
	}
	return finish()
}

// WebP decoding uses the same isolated codec path as audio/video, avoiding a
// second unrestricted decoder. Only the normalized PNG enters model context.
func normalizeWebP(ctx context.Context, data []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "molr-webp-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "input"), data, 0600); err != nil {
		return nil, err
	}
	_, err = mediaCommand(ctx, dir, "ffmpeg", "-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1", "-protocol_whitelist", "file", "-i", "input", "-frames:v", "1", "-vf", "scale=w=2048:h=2048:force_original_aspect_ratio=decrease", "-threads", "1", "image.png")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(dir, "image.png"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, mediaPartLimit+1))
}
