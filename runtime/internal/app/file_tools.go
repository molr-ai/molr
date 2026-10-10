package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/business/resource"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

const fileOutputLimit = 64 << 10
const fileScanLimit = 64 << 20
const fileMutationLimit = 1 << 20

type resourceFileTools struct {
	router           *sandboxRouter
	adapters         adapters
	resolveOverride  func(context.Context, core.Execution, tools.FileTarget, ...policy.Action) (string, string, error)
	providerOverride func(context.Context) model.Provider
	mu               sync.Mutex
	locks            map[string]*fileLock
}
type fileLock struct {
	token chan struct{}
	refs  int
}

func (f *resourceFileTools) lock(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.locks == nil {
		f.locks = map[string]*fileLock{}
	}
	l := f.locks[key]
	if l == nil {
		l = &fileLock{token: make(chan struct{}, 1)}
		l.token <- struct{}{}
		f.locks[key] = l
	}
	l.refs++
	f.mu.Unlock()
	release := func() {
		f.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(f.locks, key)
		}
		f.mu.Unlock()
	}
	select {
	case <-l.token:
		unlock := func() { l.token <- struct{}{}; release() }
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, err
		}
		return unlock, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}
func (f *resourceFileTools) resolve(ctx context.Context, e core.Execution, target tools.FileTarget, actions ...policy.Action) (string, string, error) {
	if err := checkFilePolicy(ctx, e, target, actions...); err != nil {
		return "", "", err
	}
	if f.resolveOverride != nil {
		return f.resolveOverride(ctx, e, target, actions...)
	}
	if f.router != nil {
		id, err := f.router.location(ctx, e, target)
		if err != nil {
			return "", "", err
		}
		if id != "local" {
			return "", "", core.Invalid("sandbox", "remote paths must use sandbox storage API")
		}
	}
	if err := (access{f.adapters.store}).execution(ctx, e); err != nil {
		return "", "", err
	}
	if target.ResourceID == "" && (target.Path == "/global" || strings.HasPrefix(target.Path, "/global/")) {
		if targetSandbox(target.SandboxID) != "local" || slices.Contains(actions, policy.Write) {
			return "", "", core.NewError(core.Forbidden, "global files are read-only for assistants")
		}
		root := platform.UserGlobal(f.adapters.dataDir, e.Principal.OwnerID)
		if err := os.MkdirAll(root, 0700); err != nil {
			return "", "", err
		}
		dir, err := openResourceDirectory(root, ".")
		if err != nil {
			return "", "", err
		}
		dir.Close()
		name := strings.TrimPrefix(strings.TrimPrefix(target.Path, "/global"), "/")
		if name == "" {
			name = "."
		}
		if !filepath.IsLocal(name) || path.Clean(name) != name {
			return "", "", core.Invalid("path", "canonical global path required")
		}
		return root, name, nil
	}
	if target.FullAccess {
		router := f.router
		if router == nil {
			router = &sandboxRouter{adapters: f.adapters}
		}
		if err := router.requireFullAccess(ctx, e, "local"); err != nil {
			return "", "", err
		}
		if target.ResourceID != "" {
			r, b, err := f.adapters.binding(ctx, e.Principal, target.ResourceID)
			if err != nil {
				return "", "", err
			}
			if r.Kind != resource.Storage || sandboxOf(r) != "local" {
				return "", "", denied()
			}
			if r.Provider == "managed-workspace" {
				target.Path = "/work/" + strings.TrimPrefix(target.Path, "/")
			} else {
				target.Path = filepath.Join(b.RootPath, strings.TrimPrefix(target.Path, "/"))
			}
		}
		return directFilePath(f.adapters.dataDir, f.adapters.storageConfig.WorkRoot, e, target)
	}
	if target.ResourceID != "" {
		r, err := resource.NewRepository(f.adapters.store).Get(ctx, e.Principal.OwnerID, target.ResourceID)
		if err != nil {
			return "", "", err
		}
		if r.Provider == "managed-workspace" {
			relative, err := resourcePath(target.Path)
			if err != nil {
				return "", "", err
			}
			target.ResourceID = ""
			target.Path = "/work" + strings.TrimSuffix(relative, "/")
		}
	}
	if target.ResourceID == "" {
		name, err := workspaceRelativePath(target.Path)
		if err != nil {
			return "", "", err
		}
		root := platform.AssistantWorkspace(f.adapters.dataDir, f.adapters.storageConfig.WorkRoot, e.Principal.OwnerID, e.AssistantID)
		spec, err := workspaceConstraints(ctx, f.adapters.store, e.Principal.OwnerID, e.AssistantID, "local")
		if err != nil {
			return "", "", err
		}
		if slices.Contains(actions, policy.Write) {
			if err := applyWorkspaceQuota(root, spec, f.adapters.storageConfig.WorkspaceProjectQuotas); err != nil {
				return "", "", err
			}
		}
		if err := platform.EnsureWorkspace(root); err != nil {
			return "", "", err
		}
		return root, name, nil
	}
	name, err := resourcePath(target.Path)
	if err != nil {
		return "", "", err
	}
	root := ""
	for _, action := range actions {
		b, err := f.adapters.storageBinding(ctx, e.Principal, target.ResourceID, name, action)
		if err != nil {
			return "", "", err
		}
		root = b.RootPath
	}
	return root, strings.TrimPrefix(name, "/"), nil
}
func textFile(data []byte) bool { return utf8.Valid(data) && !bytes.ContainsRune(data, 0) }
func fileToolError(code core.Code, reason core.Reason, message string) error {
	return core.WithReason(core.NewError(code, message), reason)
}

type fileReadResult struct {
	SandboxID          core.ID
	ResourceID         core.ID `json:",omitempty"`
	Path               string
	Version            string
	Text               string
	StartLine, EndLine int
	Truncated          bool
	NextOffset         int  `json:",omitempty"`
	TotalLines         *int `json:",omitempty"`
}

// Keep original line endings so old_text can be copied exactly into file_edit.
func scanFileLine(data []byte, eof bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if eof && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
func (f *resourceFileTools) readText(ctx context.Context, e core.Execution, r tools.FileReadRequest) (json.RawMessage, error) {
	if r.Offset == 0 {
		r.Offset = 1
	}
	if r.Limit == 0 {
		r.Limit = 200
	}
	if r.Offset < 1 || r.Offset > 1_000_000_000 || r.Limit < 1 || r.Limit > 2000 {
		return nil, core.Invalid("range", "invalid read range")
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
	version, err := resourceFileVersion(file)
	if err != nil {
		return nil, err
	}
	if r.ByteOffset != nil {
		if *r.ByteOffset < 0 || r.MaxBytes < 0 || r.MaxBytes > 4096 {
			return nil, core.Invalid("range", "invalid byte range")
		}
		limit := r.MaxBytes
		if limit == 0 {
			limit = 4096
		}
		if _, err = file.Seek(*r.ByteOffset, io.SeekStart); err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
		if err != nil {
			return nil, err
		}
		more := len(data) > limit
		if more {
			data = data[:limit]
		}
		if len(data) > 0 && !utf8.RuneStart(data[0]) {
			return nil, core.Invalid("byte_offset", "byte offset must start at a UTF-8 character boundary")
		}
		if !utf8.Valid(data) {
			for len(data) > 0 && !utf8.Valid(data) {
				data = data[:len(data)-1]
			}
		}
		if len(data) == 0 && more {
			return nil, core.Invalid("byte_offset", "byte offset must start at a UTF-8 character boundary")
		}
		if current, err := resourceFileVersion(file); err != nil || current != version {
			return nil, fileChanged()
		}
		if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
			return nil, err
		}
		if err = observeFileRead(ctx, e, r.FileTarget); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Path, Version, Text        string
			ByteOffset, NextByteOffset int64
			Truncated                  bool
		}{fileVirtualPath(r.FileTarget), version, string(data), *r.ByteOffset, *r.ByteOffset + int64(len(data)), more})
	}
	limited := &io.LimitedReader{R: file, N: fileScanLimit + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), fileOutputLimit+1)
	scanner.Split(scanFileLine)
	out := fileReadResult{SandboxID: "local", ResourceID: r.ResourceID, Path: fileVirtualPath(r.FileTarget), Version: version}
	var content strings.Builder
	seen, selected := 0, 0
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		seen++
		if seen < r.Offset {
			continue
		}
		line := scanner.Bytes()
		if selected == r.Limit || content.Len()+len(line) > fileOutputLimit {
			out.Truncated = true
			break
		}
		if !textFile(line) {
			return nil, fileToolError(core.InvalidArgument, "file_not_text", "file_read requires UTF-8 text; binary data is not supported")
		}
		if selected == 0 {
			out.StartLine = seen
		}
		out.EndLine = seen
		selected++
		content.Write(line)
	}
	if scanner.Err() == bufio.ErrTooLong {
		if selected == 0 {
			return nil, fileToolError(core.InvalidArgument, "file_line_too_long", "line exceeds the read window; use Shell to extract a smaller byte range")
		}
		out.Truncated = true
	} else if scanner.Err() != nil {
		return nil, core.NewError(core.Unavailable, "file read failed")
	}
	if limited.N == 0 {
		return nil, fileToolError(core.InvalidArgument, "file_scan_limit", "requested range exceeds the scan limit; narrow it or use Shell")
	}
	if out.Truncated {
		if selected == 0 {
			return nil, fileToolError(core.InvalidArgument, "file_line_too_long", "line exceeds the read window")
		}
		out.NextOffset = r.Offset + selected
	} else {
		out.TotalLines = &seen
	}
	current, err := resourceFileVersion(file)
	if err != nil {
		return nil, err
	}
	if current != version {
		return nil, fileChanged()
	}
	// Recheck live grant after reading, before exposing content.
	if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
		return nil, err
	}
	if err = observeFileRead(ctx, e, r.FileTarget); err != nil {
		return nil, err
	}
	out.Text = content.String()
	return json.Marshal(out)
}

type fileEntry struct{ Name, Path, Type string }
type fileListResult struct {
	ResourceID    core.ID `json:",omitempty"`
	Path, Version string
	Entries       []fileEntry
	Truncated     bool
	NextOffset    int `json:",omitempty"`
}

func (f *resourceFileTools) list(ctx context.Context, e core.Execution, r tools.FileListRequest) (json.RawMessage, error) {
	if f.router != nil {
		if out, remote, err := f.router.file(ctx, e, "file_list", r.FileTarget, r, ""); remote {
			return out, err
		}
	}
	if r.Limit == 0 {
		r.Limit = 100
	}
	if r.Offset < 0 || r.Offset > 100_000 || r.Limit < 1 || r.Limit > 200 || (r.Offset > 0 && r.ExpectedVersion == "") {
		return nil, core.Invalid("page", "bounded page and directory version required")
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = "."
	}
	dir, err := openResourceDirectory(root, name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	version, err := resourceFileVersion(dir)
	if err != nil {
		return nil, err
	}
	if r.ExpectedVersion != "" && r.ExpectedVersion != version {
		return nil, fileChanged()
	}
	out := fileListResult{ResourceID: r.ResourceID, Path: fileVirtualPath(r.FileTarget), Version: version, Entries: []fileEntry{}}
	position := 0
	for position < r.Offset+r.Limit+1 {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		size := min(128, r.Offset+r.Limit+1-position)
		entries, readErr := dir.ReadDir(size)
		for _, entry := range entries {
			if position >= r.Offset {
				if len(out.Entries) == r.Limit {
					out.Truncated = true
					break
				}
				kind := "other"
				if entry.IsDir() {
					kind = "directory"
				} else if entry.Type().IsRegular() {
					kind = "file"
				} else if entry.Type()&os.ModeSymlink != 0 {
					kind = "symlink"
				}
				out.Entries = append(out.Entries, fileEntry{entry.Name(), path.Join(fileVirtualPath(r.FileTarget), entry.Name()), kind})
			}
			position++
		}
		if out.Truncated {
			out.NextOffset = r.Offset + len(out.Entries)
			break
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, core.NewError(core.Unavailable, "directory listing failed")
		}
	}
	current, err := resourceFileVersion(dir)
	if err != nil {
		return nil, err
	}
	if current != version {
		return nil, fileChanged()
	}
	if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

type fileMutationResult struct {
	ResourceID    core.ID `json:",omitempty"`
	Path, Version string
	Bytes         int
	Replacements  int `json:",omitempty"`
}

func (f *resourceFileTools) write(ctx context.Context, e core.Execution, r tools.FileWriteRequest) (json.RawMessage, error) {
	if f.router != nil {
		if out, remote, err := f.router.file(ctx, e, "file_write", r.FileTarget, r, ""); remote {
			return out, err
		}
	}
	if len(r.Text) > fileMutationLimit || !textFile([]byte(r.Text)) || (!r.Overwrite && r.ExpectedVersion != "") {
		return nil, core.Invalid("text", "bounded UTF-8 text and valid overwrite options required")
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Write)
	if err != nil {
		return nil, err
	}
	unlock, err := f.lock(ctx, filepath.Join(root, name))
	if err != nil {
		return nil, err
	}
	defer unlock()
	recheck := func() error {
		current, _, err := f.resolve(ctx, e, r.FileTarget, policy.Write)
		if err != nil {
			return err
		}
		if current != root {
			return fileChanged()
		}
		return nil
	}
	if err := f.checkWorkspaceFileSize(ctx, e, r.FileTarget, int64(len(r.Text))); err != nil {
		return nil, err
	}
	if r.CreateParents {
		if err = f.makeParents(ctx, e, r.FileTarget, root, name); err != nil {
			return nil, err
		}
	}
	done, err := prepareFileMutation(ctx, e, "file_write", r.FileTarget, nil, r.ExpectedVersion, []byte(r.Text))
	if err != nil {
		return nil, err
	}
	version, err := replaceResourceFile(ctx, root, name, r.Text, r.Overwrite, r.ExpectedVersion, recheck)
	if err != nil {
		return nil, err
	}
	if err = done(); err != nil {
		return nil, core.Wrap(core.Unavailable, "file committed; history update failed", err)
	}
	return json.Marshal(fileMutationResult{ResourceID: r.ResourceID, Path: fileVirtualPath(r.FileTarget), Version: version, Bytes: len(r.Text)})
}
func (f *resourceFileTools) edit(ctx context.Context, e core.Execution, r tools.FileEditRequest) (json.RawMessage, error) {
	if f.router != nil {
		if out, remote, err := f.router.file(ctx, e, "file_edit", r.FileTarget, r, ""); remote {
			return out, err
		}
	}
	if r.ExpectedVersion == "" || r.OldText == "" || len(r.OldText) > fileMutationLimit || len(r.NewText) > fileMutationLimit || !textFile([]byte(r.OldText)) || !textFile([]byte(r.NewText)) {
		return nil, core.Invalid("edit", "bounded exact UTF-8 text and expected version required")
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read, policy.Write)
	if err != nil {
		return nil, err
	}
	unlock, err := f.lock(ctx, filepath.Join(root, name))
	if err != nil {
		return nil, err
	}
	defer unlock()
	file, err := openResourceFile(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	version, err := resourceFileVersion(file)
	if err != nil {
		return nil, err
	}
	if version != r.ExpectedVersion {
		return nil, fileChanged()
	}
	data, err := io.ReadAll(io.LimitReader(file, fileMutationLimit+1))
	if err != nil {
		return nil, core.NewError(core.Unavailable, "file edit read failed")
	}
	if len(data) > fileMutationLimit {
		return nil, fileToolError(core.InvalidArgument, "file_edit_limit", "file exceeds edit size limit; use Shell for large-file processing")
	}
	if !textFile(data) {
		return nil, fileToolError(core.InvalidArgument, "file_not_text", "file_edit requires UTF-8 text")
	}
	count := strings.Count(string(data), r.OldText)
	if count == 0 {
		return nil, fileToolError(core.Conflict, "file_edit_no_match", "old_text not found; read the file again")
	}
	if count > 1 && !r.ReplaceAll {
		return nil, fileToolError(core.Conflict, "file_edit_ambiguous", "old_text matches multiple locations; include more context or set replace_all")
	}
	if int64(len(data))+int64(count)*(int64(len(r.NewText))-int64(len(r.OldText))) > fileMutationLimit {
		return nil, core.Invalid("new_text", "edited file exceeds size limit")
	}
	replacement := strings.ReplaceAll(string(data), r.OldText, r.NewText)
	recheck := func() error {
		current, _, err := f.resolve(ctx, e, r.FileTarget, policy.Read, policy.Write)
		if err != nil {
			return err
		}
		if current != root {
			return fileChanged()
		}
		return nil
	}
	if err := f.checkWorkspaceFileSize(ctx, e, r.FileTarget, int64(len(replacement))); err != nil {
		return nil, err
	}
	done, err := prepareFileMutation(ctx, e, "file_edit", r.FileTarget, nil, r.ExpectedVersion, []byte(replacement))
	if err != nil {
		return nil, err
	}
	version, err = replaceResourceFile(ctx, root, name, replacement, true, r.ExpectedVersion, recheck)
	if err != nil {
		return nil, err
	}
	if err = done(); err != nil {
		return nil, core.Wrap(core.Unavailable, "file committed; history update failed", err)
	}
	return json.Marshal(fileMutationResult{ResourceID: r.ResourceID, Path: fileVirtualPath(r.FileTarget), Version: version, Bytes: len(replacement), Replacements: count})
}
