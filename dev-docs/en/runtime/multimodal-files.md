# On-Demand Reading of Multimodal Files

`file_read` is the unified entry point; it does not introduce media IDs or separate media tools. Attachments uploaded by users are retrieved for information via `conversation_read` and then saved to the workspace using `conversation_attachment`. Files produced by standard tools also provide only paths; media does not automatically enter the model context.

```json
{"path":"/work/screenshot.png"}
```

Authorized directories use `/resources/<resource_id>/...`. `permission_list.FilePath` and `file_list.Entries[].Path` return directly usable paths. The legacy `resource_id + path` remains compatible. Virtual paths do not expand authorization; file access continues to enforce constraints prohibiting symbolic links, hard links, special files, and out-of-bounds paths.

## Files and Model Capabilities

- UTF-8 text retains the original `offset` / `limit` line ranges, versions, and truncation information.
- JPEG, PNG, GIF, and WebP can be used as image inputs; WebP is converted to PNG via isolated decoding. GIFs are sent directly in the image format supported by the provider, with no guarantee of understanding animation timing.
- Audio is converted to mono 16 kHz WAV via isolated decoding and sent to OpenAI-compatible models that support audio input.
- Video extracts JPEG frames with sampling timestamps via isolated decoding, requiring the model to support images; it does not include audio tracks and is not equivalent to native video understanding.
- PDFs, other binaries, or media unsupported by the current model return only metadata and the reason. File types are identified based on actual content, not relying on extensions or upload MIME types.

`content_loaded=false` explicitly indicates that the model has not yet seen or heard the content. `metadata_only=true` can retrieve only metadata without loading text or media. Images support `detail=auto|low|high`; audio and video support `start_seconds` / `end_seconds`; video supports `max_frames`. Media options are not mixed with text line ranges.

JSON metadata for media reading and native media content are passed separately. Both OpenAI and DeepSeek Chat Completions adapters append associated user media messages after the complete tool response batch to avoid interrupting tool-call pairing; the existing `reasoning_content` retention mechanism for DeepSeek continues to function. This compatibility approach is adopted because the DeepSeek API definition allows tool images, but its Vision guidelines still state that only user images are supported, avoiding reliance on divergent behaviors.

## Model Configuration and Discovery

Model configuration adds `SupportsImages` / `SupportsAudio`, which can be adjusted in the WebUI model configuration window. If the discovery endpoint returns `input_modalities` and `context_window`, these fields are read; if media capabilities are not declared, they default to disabled, supporting manual configuration overrides, without guessing based on model names.

The DeepSeek adapter currently only opens image and video frame extraction; native audio is always disabled. OpenAI-compatible audio requires simultaneous audio input, text output, and tool calling capabilities; standalone transcription endpoints do not belong to this interface. Actual model capabilities for each provider still need to be verified against the deployed endpoints.

Audio also requires manually setting `AudioInputMicrosPerMillion`, where an explicit zero indicates free. The reserved budget takes the higher value of the text and audio input unit prices; settlement parses `prompt_tokens_details.audio_tokens`. If audio usage is missing, or if mixed cache usage that cannot distinguish between text/audio occurs, costs remain pending verification, without guessing bills or settling at text unit prices. Images are billed based on input tokens reported by the provider.

## Limits and Caching

- Input media files are limited to a maximum of 64 MiB; a single image or single derived media is limited to a maximum of 8 MiB.
- Direct image dimensions are limited to a maximum of 4096 per side and 40 million pixels in total (adopting the lower boundary of DeepSeek multi-image requests); WebP is subject to restricted decoding and scaled within a 2048 boundary.
- Audio and video default to reading 30 seconds, with a maximum of 60 seconds per request; video defaults to 6 frames, with a maximum of 16 frames.
- A single model request allows a maximum of 32 media content blocks and 24 MiB of media file bytes; recently read media is prioritized for retention, with earlier content replaced by unloaded prompts, which can be re-read.
- Image reading creates an immutable snapshot; subsequent overwrites of the source file do not change already-read images. Persistent context saves only source, version, processing information, and internal snapshot summaries, not Base64.
- Snapshots are located in a separate `<data_dir>/media/<owner>/<assistant>/`, not belonging to the assistant workspace. The single assistant cache limit is 512 MiB; during writes, snapshots older than 7 days or least recently used are cleaned up. Caches may be evicted; persistent references do not guarantee permanent availability.
- Source permissions, resource bindings, and current model capabilities are re-checked before each send. If the source file is deleted, authorization is revoked, the cache is missing, or the model is incompatible, only a text prompt stating "not loaded in this turn" is provided; `file_read` is required again if content is needed. It will not automatically switch models or transcribe.
- Debug traces record media references and request structures, with image and audio encoding bytes replaced by `[media omitted]`.

WeChat inbound voice messages without transcription in SILK format are first converted to WAV by an embedded, host-permission-free WebAssembly decoder; if this fails, the original audio is retained. This step does not rely on external transcription services. Model reading of WAV still follows the aforementioned audio capabilities and pricing checks.

Linux audio/video and WebP decoding require `bwrap`, `prlimit`, `ffmpeg`, and `ffprobe`. Fixed-parameter decoding runs in an isolated PID/user/mount namespace with no network access, capable of reading only system programs and the current staged files; there are limits on memory, CPU, output files, and execution time. If isolation is unavailable, metadata and the reason are returned, never falling back to host environment decoding. It does not require the assistant to obtain Shell or compute resource authorization.

## Verification

```bash
MOLR_REQUIRE_MEDIA_SANDBOX=1 go test ./internal/app -run TestMediaCodec -v
go test ./internal/agent/model -run 'TestMediaAdapters|TestAudioEncoding'
go test ./internal/app -run 'TestFileMedia|TestFileReadUnknown'
```

Official protocol references: [OpenAI Images](https://developers.openai.com/api/docs/guides/images-vision), [OpenAI Audio](https://developers.openai.com/api/docs/guides/audio-chat-completions), [DeepSeek Vision](https://api-docs.deepseek.com/guides/vision/), [DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/).
