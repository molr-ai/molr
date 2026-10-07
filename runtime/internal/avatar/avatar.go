package avatar

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"strings"

	"molr.ai/runtime/internal/core"
)

// Normalize validates and strips image metadata from an identity thumbnail.
// Keep the catalog in sync with packages/ui/src/components/AssistantAvatar.tsx.
// Uploaded thumbnails live with the identity so backups/clones are self-contained;
// they do not create orphan attachments or require public file URLs.
func Normalize(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	switch value {
	case "preset:robot", "preset:cat", "preset:fox", "preset:bear", "preset:rabbit", "preset:panda", "preset:owl", "preset:frog", "preset:penguin", "preset:whale", "preset:sprout", "preset:star":
		return value, nil
	}
	if len(value) > 128<<10 {
		return "", core.Invalid("avatar", "invalid value")
	}
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || (header != "data:image/png;base64" && header != "data:image/jpeg;base64") {
		return "", core.Invalid("avatar", "invalid value")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return "", core.Invalid("avatar", "invalid value")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 256 || config.Height > 256 || header != "data:image/"+format+";base64" {
		return "", core.Invalid("avatar", "invalid value")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", core.Invalid("avatar", "invalid value")
	}
	// Re-encode to discard metadata and any trailing non-image payload.
	var output bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&output, decoded)
	}
	if err != nil {
		return "", core.Invalid("avatar", "invalid value")
	}
	result := header + "," + base64.StdEncoding.EncodeToString(output.Bytes())
	if len(result) > 128<<10 {
		return "", core.Invalid("avatar", "invalid value")
	}
	return result, nil
}
