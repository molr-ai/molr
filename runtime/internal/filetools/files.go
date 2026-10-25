// Package filetools implements confined file operations and media preparation.
// Identity, resource resolution, quotas and remote routing are supplied by app.
package filetools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/textfiles"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Resolve must authorize the execution and apply CheckPolicy on every call.
// Mutations resolve again immediately before commit; host callbacks must not
// cache authorization across those checks. Remote may decline local requests.
type Options struct {
	DataDir   string
	Resolve   func(context.Context, core.Execution, tools.FileTarget, ...policy.Action) (string, string, error)
	Remote    func(context.Context, core.Execution, string, tools.FileTarget, any, core.ID) (json.RawMessage, bool, error)
	Provider  func(context.Context, core.Execution, core.ID) (model.Provider, error)
	Media     func(context.Context, core.Execution, media.Part) ([]byte, bool, error)
	CheckSize func(context.Context, core.Execution, tools.FileTarget, int64) error
}
type Tools struct {
	options Options
	mu      sync.Mutex
	locks   map[string]*fileLock
}

func New(o Options) *Tools { return &Tools{options: o} }
func (f *Tools) resolve(ctx context.Context, e core.Execution, t tools.FileTarget, actions ...policy.Action) (string, string, error) {
	return f.options.Resolve(ctx, e, t, actions...)
}

const OutputLimit = 64 << 10
const fileScanLimit = 64 << 20
const MutationLimit = 1 << 20

type fileLock struct {
	token chan struct{}
	refs  int
}

func (f *Tools) Lock(ctx context.Context, key string) (func(), error) {
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
func IsText(data []byte) bool { return utf8.Valid(data) && !bytes.ContainsRune(data, 0) }
func fileToolError(code core.Code, reason core.Reason, message string) error {
	return core.WithReason(core.NewError(code, message), reason)
}

type ReadResult struct {
	Diagnostics        json.RawMessage `json:",omitempty"`
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
func (f *Tools) readText(ctx context.Context, e core.Execution, r tools.FileReadRequest) (json.RawMessage, error) {
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
	file, err := openTextSource(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	outputLimit := OutputLimit
	if IsBuiltin(r.FileTarget) {
		outputLimit = 12 << 10
	}
	version, err := textSourceVersion(file)
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
		if current, err := textSourceVersion(file); err != nil || current != version {
			return nil, textfiles.Changed()
		}
		if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
			return nil, err
		}
		diagnostics, err := ObserveRead(ctx, e, r.FileTarget)
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Diagnostics                json.RawMessage `json:",omitempty"`
			Path, Version, Text        string
			ByteOffset, NextByteOffset int64
			Truncated                  bool
		}{diagnostics, VirtualPath(r.FileTarget), version, string(data), *r.ByteOffset, *r.ByteOffset + int64(len(data)), more})
	}
	limited := &io.LimitedReader{R: file, N: fileScanLimit + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), outputLimit+1)
	scanner.Split(scanFileLine)
	out := ReadResult{SandboxID: "local", ResourceID: r.ResourceID, Path: VirtualPath(r.FileTarget), Version: version}
	var content strings.Builder
	seen, selected, outputBytes := 0, 0, 0
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		seen++
		if seen < r.Offset {
			continue
		}
		line := scanner.Bytes()
		lineBytes := len(line)
		if IsBuiltin(r.FileTarget) {
			encoded, _ := json.Marshal(string(line))
			lineBytes = len(encoded) - 2
		}
		if selected == r.Limit || outputBytes+lineBytes > outputLimit {
			out.Truncated = true
			break
		}
		if !IsText(line) {
			return nil, fileToolError(core.InvalidArgument, "file_not_text", "file_read requires UTF-8 text; binary data is not supported")
		}
		if selected == 0 {
			out.StartLine = seen
		}
		out.EndLine = seen
		selected++
		content.Write(line)
		outputBytes += lineBytes
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
	current, err := textSourceVersion(file)
	if err != nil {
		return nil, err
	}
	if current != version {
		return nil, textfiles.Changed()
	}
	// Recheck live grant after reading, before exposing content.
	if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
		return nil, err
	}
	if out.Diagnostics, err = ObserveRead(ctx, e, r.FileTarget); err != nil {
		return nil, err
	}
	out.Text = content.String()
	return json.Marshal(out)
}

type fileEntry struct{ Name, Path, Type string }
type ListResult struct {
	ResourceID    core.ID `json:",omitempty"`
	Path, Version string
	Entries       []fileEntry
	Truncated     bool
	NextOffset    int `json:",omitempty"`
}

func (f *Tools) List(ctx context.Context, e core.Execution, r tools.FileListRequest) (json.RawMessage, error) {
	if f.options.Remote != nil {
		if out, remote, err := f.options.Remote(ctx, e, "file_list", r.FileTarget, r, ""); remote {
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
	dir, err := openDirectorySource(root, name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	version, err := textSourceVersion(dir)
	if err != nil {
		return nil, err
	}
	if r.ExpectedVersion != "" && r.ExpectedVersion != version {
		return nil, textfiles.Changed()
	}
	out := ListResult{ResourceID: r.ResourceID, Path: VirtualPath(r.FileTarget), Version: version, Entries: []fileEntry{}}
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
				out.Entries = append(out.Entries, fileEntry{entry.Name(), path.Join(VirtualPath(r.FileTarget), entry.Name()), kind})
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
	current, err := textSourceVersion(dir)
	if err != nil {
		return nil, err
	}
	if current != version {
		return nil, textfiles.Changed()
	}
	if _, _, err = f.resolve(ctx, e, r.FileTarget, policy.Read); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

type MutationResult struct {
	ResourceID    core.ID `json:",omitempty"`
	Path, Version string
	Bytes         int
	Replacements  int `json:",omitempty"`
}

func (f *Tools) Write(ctx context.Context, e core.Execution, r tools.FileWriteRequest) (json.RawMessage, error) {
	if f.options.Remote != nil {
		if out, remote, err := f.options.Remote(ctx, e, "file_write", r.FileTarget, r, ""); remote {
			return out, err
		}
	}
	if len(r.Text) > MutationLimit || !IsText([]byte(r.Text)) || (!r.Overwrite && r.ExpectedVersion != "") {
		return nil, core.Invalid("text", "bounded UTF-8 text and valid overwrite options required")
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Write)
	if err != nil {
		return nil, err
	}
	unlock, err := f.Lock(ctx, filepath.Join(root, name))
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
			return textfiles.Changed()
		}
		return nil
	}
	if err := f.options.CheckSize(ctx, e, r.FileTarget, int64(len(r.Text))); err != nil {
		return nil, err
	}
	if r.CreateParents {
		if err = f.makeParents(ctx, e, r.FileTarget, root, name); err != nil {
			return nil, err
		}
	}
	done, err := PrepareMutation(ctx, e, "file_write", r.FileTarget, nil, r.ExpectedVersion, []byte(r.Text))
	if err != nil {
		return nil, err
	}
	version, err := textfiles.Replace(ctx, root, name, r.Text, r.Overwrite, r.ExpectedVersion, recheck)
	if err != nil {
		return nil, err
	}
	if err = done(); err != nil {
		return nil, core.Wrap(core.Unavailable, "file committed; history update failed", err)
	}
	return json.Marshal(MutationResult{ResourceID: r.ResourceID, Path: VirtualPath(r.FileTarget), Version: version, Bytes: len(r.Text)})
}
func (f *Tools) Edit(ctx context.Context, e core.Execution, r tools.FileEditRequest) (json.RawMessage, error) {
	if f.options.Remote != nil {
		if out, remote, err := f.options.Remote(ctx, e, "file_edit", r.FileTarget, r, ""); remote {
			return out, err
		}
	}
	if r.ExpectedVersion == "" || r.OldText == "" || len(r.OldText) > MutationLimit || len(r.NewText) > MutationLimit || !IsText([]byte(r.OldText)) || !IsText([]byte(r.NewText)) {
		return nil, core.Invalid("edit", "bounded exact UTF-8 text and expected version required")
	}
	root, name, err := f.resolve(ctx, e, r.FileTarget, policy.Read, policy.Write)
	if err != nil {
		return nil, err
	}
	unlock, err := f.Lock(ctx, filepath.Join(root, name))
	if err != nil {
		return nil, err
	}
	defer unlock()
	file, err := textfiles.Open(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	version, err := textfiles.Version(file)
	if err != nil {
		return nil, err
	}
	if version != r.ExpectedVersion {
		return nil, textfiles.Changed()
	}
	data, err := io.ReadAll(io.LimitReader(file, MutationLimit+1))
	if err != nil {
		return nil, core.NewError(core.Unavailable, "file edit read failed")
	}
	if len(data) > MutationLimit {
		return nil, fileToolError(core.InvalidArgument, "file_edit_limit", "file exceeds edit size limit; use Shell for large-file processing")
	}
	if !IsText(data) {
		return nil, fileToolError(core.InvalidArgument, "file_not_text", "file_edit requires UTF-8 text")
	}
	count := strings.Count(string(data), r.OldText)
	if count == 0 {
		return nil, fileToolError(core.Conflict, "file_edit_no_match", "old_text not found; read the file again")
	}
	if count > 1 && !r.ReplaceAll {
		return nil, fileToolError(core.Conflict, "file_edit_ambiguous", "old_text matches multiple locations; include more context or set replace_all")
	}
	if int64(len(data))+int64(count)*(int64(len(r.NewText))-int64(len(r.OldText))) > MutationLimit {
		return nil, core.Invalid("new_text", "edited file exceeds size limit")
	}
	replacement := strings.ReplaceAll(string(data), r.OldText, r.NewText)
	recheck := func() error {
		current, _, err := f.resolve(ctx, e, r.FileTarget, policy.Read, policy.Write)
		if err != nil {
			return err
		}
		if current != root {
			return textfiles.Changed()
		}
		return nil
	}
	if err := f.options.CheckSize(ctx, e, r.FileTarget, int64(len(replacement))); err != nil {
		return nil, err
	}
	done, err := PrepareMutation(ctx, e, "file_edit", r.FileTarget, nil, r.ExpectedVersion, []byte(replacement))
	if err != nil {
		return nil, err
	}
	version, err = textfiles.Replace(ctx, root, name, replacement, true, r.ExpectedVersion, recheck)
	if err != nil {
		return nil, err
	}
	if err = done(); err != nil {
		return nil, core.Wrap(core.Unavailable, "file committed; history update failed", err)
	}
	return json.Marshal(MutationResult{ResourceID: r.ResourceID, Path: VirtualPath(r.FileTarget), Version: version, Bytes: len(replacement), Replacements: count})
}
