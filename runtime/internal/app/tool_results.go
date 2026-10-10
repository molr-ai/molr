package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
)

const toolInlineLimit = 16 << 10

func previewText(data []byte, limit int) string {
	for len(data) > 0 && !utf8.RuneStart(data[0]) {
		data = data[1:]
	}
	if len(data) > limit {
		data = data[:limit]
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data)
}
func (f *resourceFileTools) boundResult(ctx context.Context, e core.Execution, result tools.Result) (tools.Result, error) {
	// Output artifacts are Runtime-owned writes to a fixed directory, not
	// model-requested file mutations. Keep identity and deadlines intact.
	ctx = context.WithValue(ctx, filePolicyKey{}, (*filePolicy)(nil))
	if len(result.Content) <= toolInlineLimit {
		return result, nil
	}
	sum := sha256.Sum256(result.Content)
	// Content addressing makes repeated receipt delivery reuse the same artifact.
	name := platform.WorkspaceStateDirectory + "/tool-result-" + hex.EncodeToString(sum[:]) + ".json"
	root, _, err := f.resolve(ctx, e, tools.FileTarget{Path: name}, policy.Write)
	if err != nil {
		return result, err
	}
	if err := textfiles.MkdirAll(root, platform.WorkspaceStateDirectory); err != nil {
		return result, err
	}
	unlock, err := f.lock(ctx, root+"/"+name)
	if err != nil {
		return result, err
	}
	defer unlock()
	if existing, openErr := openResourceFile(root, name); openErr == nil {
		data, readErr := io.ReadAll(io.LimitReader(existing, int64(len(result.Content))+1))
		existing.Close()
		if readErr != nil || !bytes.Equal(data, result.Content) {
			return result, core.NewError(core.Conflict, "saved tool result was changed; original receipt retained")
		}
	} else if core.CodeOf(openErr) == core.NotFound {
		_, err = replaceResourceFile(ctx, root, name, string(result.Content), false, "", func() error { _, _, err := f.resolve(ctx, e, tools.FileTarget{Path: name}, policy.Write); return err })
		if err != nil {
			return result, err
		}
	} else {
		return result, openErr
	}
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(result.Content, &fields)
	details := map[string]json.RawMessage{}
	// Keep bounded structural evidence, especially failure/exit state and paging.
	detailBytes := 0
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := fields[key]
		switch strings.ToLower(key) {
		case "sandboxid", "exit", "error", "ok", "operation_id", "path", "targetkind", "version", "nextoffset", "nextbyteoffset", "truncated", "total", "totalcount", "count", "hasmore", "nextcursor", "outputcomplete", "stdoutpath", "stderrpath", "stdoutbytes", "stderrbytes":
			encoded, _ := json.Marshal(map[string]json.RawMessage{key: value})
			if len(value) <= 512 && detailBytes+len(encoded) < 2048 {
				detailBytes += len(encoded)
				details[key] = value
			}
		}
	}
	content, err := json.Marshal(struct {
		Summary        string                     `json:"summary"`
		Preview        string                     `json:"preview"`
		Tail           string                     `json:"tail"`
		FullResultPath string                     `json:"full_result_path"`
		Bytes          int                        `json:"bytes"`
		SHA256         string                     `json:"sha256"`
		Details        map[string]json.RawMessage `json:"details,omitempty"`
		Read           string                     `json:"read"`
	}{resultSummary(fields, len(result.Content)), previewText(result.Content, 1024), previewText(result.Content[max(0, len(result.Content)-512):], 512), "/work/" + name, len(result.Content), hex.EncodeToString(sum[:]), details, "Use file_read with this path, byte_offset=0 and max_bytes=4096; continue at NextByteOffset. Do not repeat the original operation to retrieve output."})
	if err != nil {
		return result, err
	}
	if len(content) > toolInlineLimit {
		return result, core.NewError(core.Internal, "tool result envelope exceeded limit")
	}
	result.Content = content
	return result, nil
}

// This summary is deterministic metadata, not an invented semantic conclusion.
func resultSummary(fields map[string]json.RawMessage, size int) string {
	parts := []string{fmt.Sprintf("Full result saved (%d bytes).", size)}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := fields[key]
		if len(value) > 0 && value[0] == '[' {
			var rows []json.RawMessage
			if json.Unmarshal(value, &rows) == nil {
				parts = append(parts, fmt.Sprintf("%s: %d items.", key, len(rows)))
			}
		} else if len(value) > 512 {
			parts = append(parts, fmt.Sprintf("%s: %d encoded bytes.", key, len(value)))
		}
		if len(strings.Join(parts, " ")) > 512 {
			break
		}
	}
	parts = append(parts, "Execution/paging metadata and excerpts follow; read the file for complete content.")
	return previewText([]byte(strings.Join(parts, " ")), 512)
}
