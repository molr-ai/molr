package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
)

type FileTarget struct {
	FullAccess bool    `json:"full_access,omitempty"`
	SandboxID  core.ID `json:"sandbox_id,omitempty"`
	ResourceID core.ID `json:"resource_id,omitempty"`
	Path       string  `json:"path"`
}
type FileReadRequest struct {
	ByteOffset   *int64   `json:"byte_offset,omitempty"`
	MaxBytes     int      `json:"max_bytes,omitempty"`
	ModelID      core.ID  `json:"-"`
	TextRange    bool     `json:"-"`
	MetadataOnly bool     `json:"metadata_only,omitempty"`
	Detail       string   `json:"detail,omitempty"`
	StartSeconds *float64 `json:"start_seconds,omitempty"`
	EndSeconds   *float64 `json:"end_seconds,omitempty"`
	MaxFrames    int      `json:"max_frames,omitempty"`
	FileTarget
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`
}
type FileListRequest struct {
	FileTarget
	Offset          int    `json:"offset,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	ExpectedVersion string `json:"expected_version,omitempty"`
}
type FileWriteRequest struct {
	CreateParents bool `json:"create_parents,omitempty"`
	FileTarget
	Text            string `json:"text"`
	Overwrite       bool   `json:"overwrite"`
	ExpectedVersion string `json:"expected_version,omitempty"`
}
type FileEditRequest struct {
	FileTarget
	OldText         string `json:"old_text"`
	NewText         string `json:"new_text"`
	ExpectedVersion string `json:"expected_version"`
	ReplaceAll      bool   `json:"replace_all,omitempty"`
}

type FileMoveRequest struct {
	FileTarget
	ToPath          string `json:"to_path"`
	ExpectedVersion string `json:"expected_version"`
	CreateParents   bool   `json:"create_parents,omitempty"`
}
type FileDeleteRequest struct {
	FileTarget
	ExpectedVersion string `json:"expected_version"`
}

func fileArguments(call Call) (arguments, error) {
	var args arguments
	if len(call.Arguments) > 3<<20 {
		return args, core.Invalid("arguments", "file arguments too large")
	}
	var target *FileTarget
	var value any
	required := []string{"path"}
	switch call.Name {
	case "file_search":
		args.fileSearch = &FileSearchRequest{}
		target = &args.fileSearch.FileTarget
		value = args.fileSearch
	case "file_read":
		args.fileRead = &FileReadRequest{}
		target = &args.fileRead.FileTarget
		value = args.fileRead
	case "file_list":
		args.fileList = &FileListRequest{}
		target = &args.fileList.FileTarget
		value = args.fileList
	case "file_write":
		args.fileWrite = &FileWriteRequest{}
		target = &args.fileWrite.FileTarget
		value = args.fileWrite
		required = append(required, "text", "overwrite")
	case "file_move":
		args.fileMove = &FileMoveRequest{}
		target = &args.fileMove.FileTarget
		value = args.fileMove
		required = append(required, "to_path", "expected_version")
	case "file_delete":
		args.fileDelete = &FileDeleteRequest{}
		target = &args.fileDelete.FileTarget
		value = args.fileDelete
		required = append(required, "expected_version")
	case "file_edit":
		args.fileEdit = &FileEditRequest{}
		target = &args.fileEdit.FileTarget
		value = args.fileEdit
		required = append(required, "old_text", "new_text", "expected_version")
	default:
		return args, core.Invalid("tool", "unknown file tool")
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return args, core.Invalid("arguments", "invalid file arguments")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return args, core.Invalid("arguments", "trailing data")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(call.Arguments, &fields) != nil || fields == nil {
		return args, core.Invalid("arguments", "file arguments required")
	}
	for _, name := range required {
		if raw, ok := fields[name]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return args, core.Invalid(name, "required")
		}
	}
	for _, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return args, core.Invalid("arguments", "null file arguments are not supported")
		}
	}
	if target.SandboxID != "" && target.SandboxID.Validate() != nil {
		return args, core.Invalid("sandbox_id", "invalid sandbox ID")
	}
	if len(target.Path) > 4096 {
		return args, core.Invalid("path", "path exceeds limit")
	}
	if strings.HasPrefix(target.Path, "/resources/") {
		parts := strings.SplitN(strings.TrimPrefix(target.Path, "/resources/"), "/", 2)
		id := core.ID(parts[0])
		if target.ResourceID != "" && target.ResourceID != id {
			return args, core.Invalid("resource_id", "resource_id conflicts with virtual path")
		}
		target.ResourceID = id
		if target.ResourceID.Validate() != nil {
			return args, core.Invalid("path", "invalid resource path")
		}
		target.Path = "/"
		if len(parts) == 2 {
			target.Path += parts[1]
		}
	}
	canonical := "/" + strings.TrimPrefix(target.Path, "/")
	if target.Path == "" || len(target.Path) > 4096 || strings.ContainsAny(target.Path, "\x00\\") {
		return args, core.Invalid("path", "canonical file path required")
	}
	if _, provided := fields["resource_id"]; provided && target.ResourceID.Validate() != nil {
		return args, core.Invalid("resource_id", "omit resource_id for the assistant workspace; otherwise provide a valid resource ID")
	}
	if target.ResourceID == "" {
		if target.Path == "." || target.Path == "/work" {
			canonical = "/work"
		} else if !strings.HasPrefix(target.Path, "/") {
			canonical = "/work/" + target.Path
		}

	}
	if path.Clean(canonical) != canonical {
		return args, core.Invalid("path", "canonical confined path required")
	}
	if call.Name != "file_list" && call.Name != "file_search" && (canonical == "/" || (target.ResourceID == "" && canonical == "/work")) {
		return args, core.Invalid("path", "file path required; use file_list for directories")
	}
	if args.fileSearch != nil {
		if err := args.fileSearch.Validate(); err != nil {
			return args, err
		}
	}
	if r := args.fileRead; r != nil {
		r.TextRange = fields["offset"] != nil || fields["limit"] != nil || r.ByteOffset != nil
		if (r.ByteOffset != nil && (*r.ByteOffset < 0 || fields["offset"] != nil || fields["limit"] != nil || r.MetadataOnly)) || r.MaxBytes < 0 || r.MaxBytes > 4096 || (r.MaxBytes != 0 && r.ByteOffset == nil) {
			return args, core.Invalid("range", "use byte_offset with max_bytes (1..4096), without line ranges or metadata_only")
		}
		if r.Detail != "" && r.Detail != "auto" && r.Detail != "low" && r.Detail != "high" {
			return args, core.Invalid("detail", "use auto, low or high")
		}
		if r.MaxFrames < 0 || r.MaxFrames > 16 {
			return args, core.Invalid("max_frames", "maximum 16 frames")
		}
		if r.StartSeconds != nil && (*r.StartSeconds < 0 || *r.StartSeconds > 86400) {
			return args, core.Invalid("start_seconds", "invalid start")
		}
		if r.EndSeconds != nil && (*r.EndSeconds <= 0 || *r.EndSeconds > 86400 || (r.StartSeconds != nil && *r.EndSeconds <= *r.StartSeconds)) {
			return args, core.Invalid("end_seconds", "end must follow start")
		}
		if (r.MetadataOnly || r.Detail != "" || r.StartSeconds != nil || r.EndSeconds != nil || r.MaxFrames != 0) && (fields["offset"] != nil || fields["limit"] != nil) {
			return args, core.Invalid("range", "line ranges cannot be combined with media options")
		}
		if r.Offset < 0 || r.Offset > 1_000_000_000 || r.Limit < 0 || r.Limit > 2000 {
			return args, core.Invalid("range", "offset 1..1000000000 and limit 1..2000 required")
		}
		if r.Offset == 0 {
			if _, ok := fields["offset"]; ok {
				return args, core.Invalid("offset", "offset starts at 1")
			}
			r.Offset = 1
		}
		if r.Limit == 0 {
			if _, ok := fields["limit"]; ok {
				return args, core.Invalid("limit", "positive limit required")
			}
			r.Limit = 200
		}
	}
	if r := args.fileList; r != nil {
		if r.Offset < 0 || r.Offset > 100_000 || r.Limit < 0 || r.Limit > 200 || len(r.ExpectedVersion) > 128 {
			return args, core.Invalid("page", "bounded directory page required")
		}
		if r.Limit == 0 {
			if _, ok := fields["limit"]; ok {
				return args, core.Invalid("limit", "positive limit required")
			}
			r.Limit = 100
		}
		if r.Offset > 0 && r.ExpectedVersion == "" {
			return args, core.Invalid("expected_version", "use the directory version returned by the first page")
		}
	}
	if r := args.fileWrite; r != nil {
		if !fileText(r.Text) || len(r.ExpectedVersion) > 128 || (!r.Overwrite && r.ExpectedVersion != "") {
			return args, core.Invalid("write", "bounded UTF-8 text and valid overwrite options required")
		}
	}
	if r := args.fileEdit; r != nil {
		if !fileText(r.OldText) || !fileText(r.NewText) || r.OldText == "" || r.ExpectedVersion == "" || len(r.ExpectedVersion) > 128 {
			return args, core.Invalid("edit", "nonempty old text and expected version required; text must be bounded UTF-8")
		}
	}
	if r := args.fileMove; r != nil {
		// Normalize the destination using the same target parser. Moves cannot
		// cross resource or sandbox boundaries; file_transfer handles copies.
		dest := *target
		dest.Path = r.ToPath
		raw, _ := json.Marshal(struct {
			FileTarget
			Text      string `json:"text"`
			Overwrite bool   `json:"overwrite"`
		}{dest, "", false})
		parsed, err := fileArguments(Call{Name: "file_write", Arguments: raw})
		if err != nil {
			return args, err
		}
		if parsed.ResourceID != target.ResourceID || parsed.Path == canonical {
			return args, core.Invalid("to_path", "distinct destination in the same resource required")
		}
		r.ToPath = parsed.Path
		if r.ExpectedVersion == "" || len(r.ExpectedVersion) > 128 {
			return args, core.Invalid("expected_version", "current file version required")
		}
	}
	if r := args.fileDelete; r != nil && (r.ExpectedVersion == "" || len(r.ExpectedVersion) > 128) {
		return args, core.Invalid("expected_version", "current file version required")
	}
	args.ResourceID = target.ResourceID
	args.Path = canonical
	target.Path = canonical
	return args, nil
}

func fileText(s string) bool {
	return len(s) <= 1<<20 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func (e *executor) fileDefinitions() []Definition {
	ref := `"full_access":{"type":"boolean","description":"Explicitly request unrestricted access using an active Full Access compute grant; false defaults to isolated file permissions"},"sandbox_id":{"type":"string","description":"Omit for the local computer. Remote operations require explicit sandbox_id and an authorized directory resource_id; remote computers have no workspace"},"resource_id":{"type":"string","description":"Optional external storage ID; omit for your own workspace"},"path":{"type":"string","maxLength":4096}`
	version := `"expected_version":{"type":"string","maxLength":128}`
	base := " /molr contains built-in read-only Molr documentation accessible with file_read, file_list and file_search without sandbox or network access. Use absolute /molr paths, omit resource_id and sandbox_id, and do not set full_access; these files are not Shell paths. An active compute grant with Scope.FullAccess=true permits full_access=true calls to access absolute filesystem paths and links without separate storage grants. Omit full_access to keep normal file permissions; it never upgrades automatically.  Omitting sandbox_id always means the local computer. Remote computers require explicit sandbox_id and have no assistant workspace. By default, operate on your own local persistent workspace: omit resource_id and use a relative path or /work/..., with . or /work for its root. Workspace access is built in and needs no storage or compute grant. The local /global directory contains your owner’s shared instructions, skills and memories; it is read-only and the same files are visible to local shell commands. For external storage, use /resources/<resource_id>/... from permission_list, or legacy resource_id plus a resource-relative path; / then means that resource root. Other assistants, host paths and /tmp are not reachable through workspace paths. Access is rechecked at execution."
	defs := []Definition{}
	if e.options.FileSearch != nil {
		defs = append(defs, Definition{Name: "file_search", Description: "Search UTF-8 file content or find paths recursively. Literal matching by default; regex uses RE2, line-by-line. mode=files returns matching paths; omit pattern in files mode to enumerate paths selected by globs. Glob supports **, *, ? and character classes; no .gitignore interpretation. Hidden descendants are skipped unless include_hidden; an explicitly selected hidden root is searched. No discovered links are followed. Results include paths, line numbers, bounded context, scan/skipped counts and Complete/Truncated/StopReason. Limits: 10,000 entries, depth 32, 2 MiB per file, 64 MiB total, 5 seconds, 32 KiB results. Incomplete results are not proof of absence. Narrow the directory or pattern when truncated. Read the current file before editing." + base, InputSchema: schema(ref+`,"pattern":{"type":"string","maxLength":2048},"regex":{"type":"boolean"},"case_sensitive":{"type":"boolean"},"glob":{"type":"array","maxItems":32,"items":{"type":"string"}},"exclude_glob":{"type":"array","maxItems":32,"items":{"type":"string"}},"mode":{"enum":["content","files"]},"context_lines":{"type":"integer","minimum":0,"maximum":5},"include_hidden":{"type":"boolean"},"max_results":{"type":"integer","minimum":1,"maximum":200}`, `"path"`), RequiredActions: []string{"read"}})
	}
	if e.options.FileRead != nil {
		defs = append(defs, Definition{Name: "file_read", Description: "Read UTF-8 text with line ranges, version and explicit truncation. offset is 1-based; default limit is 200, maximum 2000. Text pages are limited to 64 KiB; results over 16 KiB are saved as workspace files. Long tool results are saved in the workspace; use byte_offset and max_bytes for bounded reads of result files or very long lines. Use NextOffset to continue; oversized individual lines are rejected. Binary files return metadata. Images and audio are loaded only when the current model supports them; videos use bounded timestamped frames when image input is supported (no audio). content_loaded=false means you have NOT seen or heard the content. Use metadata_only to inspect without loading. Default audio/video window is 30 seconds; maximum 60 seconds, maximum 16 video frames. Only metadata_only is also valid for text; detail and time/frame options require media. For chat attachments, first save with conversation_attachment." + base, InputSchema: schema(ref+`,"byte_offset":{"type":"integer","minimum":0},"max_bytes":{"type":"integer","minimum":1,"maximum":4096},"metadata_only":{"type":"boolean"},"detail":{"enum":["auto","low","high"]},"start_seconds":{"type":"number","minimum":0},"end_seconds":{"type":"number","exclusiveMinimum":0},"max_frames":{"type":"integer","minimum":1,"maximum":16},"offset":{"type":"integer","minimum":1,"maximum":1000000000},"limit":{"type":"integer","minimum":1,"maximum":2000}`, `"path"`), RequiredActions: []string{"read"}})
	}
	if e.options.FileList != nil {
		defs = append(defs, Definition{Name: "file_list", Description: "List a workspace or authorized storage directory without recursion or following symlinks. Default limit 100, maximum 200. Entries use filesystem order. Continue using NextOffset and the returned Version as expected_version; directory changes require restarting pagination." + base, InputSchema: schema(ref+`,"offset":{"type":"integer","minimum":0,"maximum":100000},"limit":{"type":"integer","minimum":1,"maximum":200},`+version, `"path"`), RequiredActions: []string{"read"}})
	}
	if e.options.FileWrite != nil {
		defs = append(defs, Definition{Name: "file_write", Description: "Create or fully replace a UTF-8 file, up to 1 MiB, using atomic replacement. overwrite is required: false never replaces an existing file; true explicitly allows replacement. Pass expected_version from file_read to prevent overwriting changes since that read. Set create_parents=true to create missing authorized parent directories. Use file_edit for targeted changes." + base, InputSchema: schema(ref+`,"text":{"type":"string","maxLength":1048576},"overwrite":{"type":"boolean"},"create_parents":{"type":"boolean"},`+version, `"path","text","overwrite"`), RequiredActions: []string{"write"}, SideEffect: true})
	}
	if e.options.FileEdit != nil {
		defs = append(defs, Definition{Name: "file_edit", Description: "Atomically replace exact old_text with new_text in a UTF-8 file up to 1 MiB. Requires read and write permission and expected_version from file_read. Missing/stale matches fail without changes; multiple matches fail unless replace_all=true. No regex or fuzzy matching." + base, InputSchema: schema(ref+`,"old_text":{"type":"string","minLength":1,"maxLength":1048576},"new_text":{"type":"string","maxLength":1048576},"replace_all":{"type":"boolean"},`+version, `"path","old_text","new_text","expected_version"`), RequiredActions: []string{"read", "write"}, SideEffect: true})
	}
	if e.options.FileMove != nil {
		defs = append(defs, Definition{Name: "file_move", Description: "Atomically move or rename one regular file to an absent path in the same workspace or storage resource. Requires write permission on both source and destination and the current expected_version from file_read. Destination is interpreted in the same resource as path; cross-resource moves and directory moves are not supported. Set create_parents=true to create missing authorized destination parents. No existing destination is overwritten." + base, InputSchema: schema(ref+`,"to_path":{"type":"string","maxLength":4096},"create_parents":{"type":"boolean"},`+version, `"path","to_path","expected_version"`), RequiredActions: []string{"write"}, SideEffect: true})
	}
	if e.options.FileDelete != nil {
		defs = append(defs, Definition{Name: "file_delete", Description: "Delete one regular file using its current expected_version from file_read. Requires write permission. Does not remove directories, recurse, follow symbolic links or erase source history. A stale version leaves the file untouched." + base, InputSchema: schema(ref+`,`+version, `"path","expected_version"`), RequiredActions: []string{"write"}, SideEffect: true})
	}
	return defs
}
