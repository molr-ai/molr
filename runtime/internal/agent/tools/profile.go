package tools

import (
	"bytes"
	"encoding/json"
	"io"

	"molr.ai/runtime/internal/core"
)

type ProfileAvatarRequest struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	FileTarget
}
type ProfileUpdateRequest struct {
	Name            *string               `json:"name,omitempty"`
	Avatar          *ProfileAvatarRequest `json:"avatar,omitempty"`
	ExpectedVersion core.Version          `json:"expected_version"`
}

func profileDefinition() Definition {
	return Definition{Name: "profile_update", Description: "Update your own display name and/or avatar atomically. Omitted fields stay unchanged. Use expected_version from the current profile runtime context or the last result. On conflict inspect current_profile; do not blindly overwrite a user's edit. Names are trimmed, 1–256 characters, with no control characters. Avatars: preset (robot, cat, fox, bear, rabbit, panda, owl, frog, penguin, whale, sprout, star), file (PNG/JPEG/WebP up to 10 MiB and 40 million pixels, center-cropped to 256×256), or clear (name initial). File paths use the local workspace or local authorized resources. Download remote images using file_transfer first; chat attachments must first be saved using conversation_attachment. URLs and base64 are not accepted. Only name and avatar change, never your model, permissions or budget. Changes take effect immediately. Check the result before announcing success via conversation_publish.", InputSchema: schema(`"name":{"type":"string","minLength":1,"maxLength":256},"expected_version":{"type":"integer","minimum":1},"avatar":{"type":"object","properties":{"type":{"enum":["preset","file","clear"]},"id":{"enum":["robot","cat","fox","bear","rabbit","panda","owl","frog","penguin","whale","sprout","star"]},"path":{"type":"string","maxLength":4096},"sandbox_id":{"enum":["local"]},"resource_id":{"type":"string"}},"required":["type"],"additionalProperties":false}`, `"expected_version"`), RequiredActions: []string{"write"}, SideEffect: true}
}

func parseProfile(raw json.RawMessage) (ProfileUpdateRequest, error) {
	var r ProfileUpdateRequest
	if len(raw) > 16<<10 {
		return r, core.Invalid("arguments", "profile arguments too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil {
		return r, core.Invalid("arguments", "invalid profile arguments")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return r, core.Invalid("arguments", "trailing data")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return r, core.Invalid("arguments", "profile object required")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return r, core.Invalid("arguments", "omit unchanged fields instead of null")
		}
	}
	if r.ExpectedVersion == 0 {
		return r, core.Invalid("expected_version", "current profile version required")
	}
	if r.Name == nil && r.Avatar == nil {
		return r, core.Invalid("profile", "name or avatar required")
	}
	if r.Avatar != nil {
		a := r.Avatar
		var avatarFields map[string]json.RawMessage
		_ = json.Unmarshal(fields["avatar"], &avatarFields)
		for k, v := range avatarFields {
			if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return r, core.Invalid("avatar", "null avatar fields are not supported")
			}
			if k != "type" && !((a.Type == "preset" && k == "id") || (a.Type == "file" && (k == "path" || k == "sandbox_id" || k == "resource_id"))) {
				return r, core.Invalid("avatar", "fields do not match avatar type")
			}
		}
		switch a.Type {
		case "preset":
			switch a.ID {
			case "robot", "cat", "fox", "bear", "rabbit", "panda", "owl", "frog", "penguin", "whale", "sprout", "star":
			default:
				return r, core.Invalid("avatar.id", "unknown preset")
			}
		case "file":
			delete(avatarFields, "type")
			args, _ := json.Marshal(avatarFields)
			parsed, err := fileArguments(Call{Name: "file_read", Arguments: args})
			if err != nil {
				return r, err
			}
			a.FileTarget = parsed.fileRead.FileTarget
			if a.SandboxID != "" && a.SandboxID != "local" {
				return r, core.Invalid("sandbox_id", "avatar files must be local")
			}
		case "clear":
		default:
			return r, core.Invalid("avatar.type", "use preset, file or clear")
		}
	}
	return r, nil
}
