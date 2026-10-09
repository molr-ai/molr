package debugtrace

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

var credentialPattern = regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/=-]+|\bsk-[a-z0-9_-]{12,}`)

// Replacer is safe for concurrent use; its compiled table is shared by captures.
var credentialKeySeparators = strings.NewReplacer("_", "", "-", "")

func sanitize(value any, limit int) (json.RawMessage, string) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, "unavailable"
	}
	if len(data) > limit {
		return nil, "too_large"
	}
	var tree any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&tree) != nil {
		return nil, "unavailable"
	}
	redacted := false
	var walk func(any, int) any
	walk = func(v any, depth int) any {
		if depth > 64 {
			redacted = true
			return "[REDACTED: depth limit]"
		}
		switch value := v.(type) {
		case map[string]any:
			for key, item := range value {
				normalized := strings.ToLower(credentialKeySeparators.Replace(key))
				switch normalized {
				case "authorization", "apikey", "password", "secret", "accesstoken", "refreshtoken", "cookie", "setcookie":
					value[key] = "[REDACTED]"
					redacted = true
				default:
					value[key] = walk(item, depth+1)
				}
			}
		case []any:
			for i, item := range value {
				value[i] = walk(item, depth+1)
			}
		case string:
			var nested any
			nestedDecoder := json.NewDecoder(strings.NewReader(value))
			nestedDecoder.UseNumber()
			if (strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[")) && nestedDecoder.Decode(&nested) == nil {
				encoded, _ := json.Marshal(walk(nested, depth+1))
				value = string(encoded)
			}
			clean := credentialPattern.ReplaceAllString(value, "[REDACTED]")
			if clean != value {
				redacted = true
			}
			return clean
		}
		return v
	}
	data, err = json.Marshal(walk(tree, 0))
	if err != nil {
		return nil, "unavailable"
	}
	if len(data) > limit {
		return nil, "too_large"
	}
	if redacted {
		return data, "redacted"
	}
	return data, "captured"
}
