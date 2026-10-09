package memory

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

// FileSnapshot exposes the actual Markdown bytes, including optional frontmatter.
// Versions are content hashes, shared with the memory UI.
type FileSnapshot struct {
	Path, Text, Version string
	FileVersion         string `json:"-"`
	ReadOnly            bool
	Sources             []core.Object `json:"-"`
}

type FileChange struct {
	Ref                       Ref
	Before, After             []byte
	BeforeExists, AfterExists bool
}
type fileJournal struct {
	Digest  string
	Sources []core.Object
	Changes []FileChange
	Done    bool
	Undone  bool
}

func (f *Files) fileJournalPath(owner, operation core.ID) (string, string) {
	return filepath.Join(f.options.JournalRoot, hash([]byte(owner))), "dream-file-" + hash([]byte(operation)) + ".json"
}
func (f *Files) ReadFile(ctx context.Context, owner core.ID, ref Ref) (FileSnapshot, error) {
	if err := ref.Validate(false); err != nil {
		return FileSnapshot{}, err
	}
	root, err := f.root(ctx, Memory{Ref: ref, OwnerID: owner})
	if err != nil {
		return FileSnapshot{}, err
	}
	raw, fileVersion, _, err := readRaw(root.Directory, ref.Path, 80<<10)
	if err != nil {
		return FileSnapshot{}, err
	}
	var m Memory
	if err = decode(raw, &m); err != nil {
		return FileSnapshot{}, err
	}
	sources, _, err := f.fileSources(ctx, owner, ref, hash(raw), m.Sources)
	if err != nil {
		return FileSnapshot{}, err
	}
	return FileSnapshot{Path: ref.LogicalPath(), Text: string(raw), Version: hash(raw), ReadOnly: ref.Scope == Global || m.Confirmed, Sources: sources, FileVersion: fileVersion}, nil
}

// PrepareFileOperation records an undo image before the common file tool acts.
// It never changes a workspace file or executes a model request.
func (f *Files) PrepareFileOperation(ctx context.Context, owner, operation core.ID, digest string, changes []FileChange, check func() error, sources ...core.Object) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.options.JournalRoot == "" || len(changes) == 0 {
		return core.NewError(core.Unavailable, "file journal unavailable")
	}
	dir, name := f.fileJournalPath(owner, operation)
	record := fileJournal{Digest: digest, Changes: changes, Sources: sources}
	if raw, _, _, err := readRaw(dir, name, 512<<10); err == nil {
		var prior fileJournal
		if json.Unmarshal(raw, &prior) != nil || prior.Digest != digest || prior.Undone {
			return conflict()
		}
		return nil
	} else if core.CodeOf(err) != core.NotFound {
		return err
	}
	for _, c := range changes {
		if err := c.Ref.Validate(false); err != nil {
			return err
		}
		if c.Ref.Scope != Assistant {
			return forbidden()
		}
		if c.AfterExists {
			m := Memory{Ref: c.Ref, OwnerID: owner}
			if len(c.After) > 80<<10 {
				return invalid("text")
			}
			if err := decode(c.After, &m); err != nil {
				return err
			}
			if m.Confirmed {
				return core.NewError(core.Forbidden, "dream cannot confirm a memory")
			}
			m.Sources = append(m.Sources, sources...)
			suppressed, err := f.Suppressed(ctx, m)
			if err != nil {
				return err
			}
			if suppressed {
				return core.NewError(core.Conflict, "forgotten memory cannot be restored")
			}
			if c.Ref.Path == "MEMORY.md" && len(m.Text) > 6<<10 {
				return invalid("core_size")
			}
		}
		if c.BeforeExists {
			var m Memory
			if err := decode(c.Before, &m); err != nil {
				return err
			}
			if m.Confirmed {
				return core.NewError(core.Forbidden, "confirmed memory is read-only")
			}
		}
		if err := f.matchFile(ctx, owner, c.Ref, c.Before, c.BeforeExists); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, _ := json.Marshal(record)
	_, err := textfiles.Replace(ctx, dir, name, string(data), false, "", check)
	return err
}
func (f *Files) CompleteFileOperation(ctx context.Context, owner, operation core.ID, check func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, name := f.fileJournalPath(owner, operation)
	raw, stat, _, err := readRaw(dir, name, 512<<10)
	if err != nil {
		return err
	}
	var record fileJournal
	if json.Unmarshal(raw, &record) != nil || record.Undone {
		return conflict()
	}
	if record.Done {
		return nil
	}
	for _, c := range record.Changes {
		if err = f.matchFile(ctx, owner, c.Ref, c.After, c.AfterExists); err != nil {
			return err
		}
	}
	record.Done = true
	data, _ := json.Marshal(record)
	_, err = textfiles.Replace(ctx, dir, name, string(data), true, stat, check)
	return err
}
func (f *Files) matchFile(ctx context.Context, owner core.ID, ref Ref, want []byte, exists bool) error {
	root, err := f.root(ctx, Memory{Ref: ref, OwnerID: owner})
	if err != nil {
		return err
	}
	data, _, _, err := readRaw(root.Directory, ref.Path, 80<<10)
	if !exists && core.CodeOf(err) == core.NotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if !exists || hash(data) != hash(want) {
		return conflict()
	}
	return nil
}

func (f *Files) UndoFileOperation(ctx context.Context, owner, operation core.ID, check func() error) ([]Ref, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, name := f.fileJournalPath(owner, operation)
	raw, journalStat, _, err := readRaw(dir, name, 512<<10)
	if core.CodeOf(err) == core.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r fileJournal
	if json.Unmarshal(raw, &r) != nil {
		return nil, invalid("file_journal")
	}
	if r.Undone {
		return nil, nil
	}
	retained := []Ref{}
	// Check the complete operation before undoing either half of a move.
	for _, c := range r.Changes {
		if f.matchFile(ctx, owner, c.Ref, c.Before, c.BeforeExists) == nil {
			continue
		}
		if f.matchFile(ctx, owner, c.Ref, c.After, c.AfterExists) != nil {
			for _, v := range r.Changes {
				retained = append(retained, v.Ref)
			}
			return retained, nil
		}
	}
	for i := len(r.Changes) - 1; i >= 0; i-- {
		c := r.Changes[i]
		if f.matchFile(ctx, owner, c.Ref, c.Before, c.BeforeExists) == nil {
			continue
		}
		root, err := f.root(ctx, Memory{Ref: c.Ref, OwnerID: owner})
		if err != nil {
			return nil, err
		}
		stat := ""
		if c.AfterExists {
			var actual []byte
			actual, stat, _, err = readRaw(root.Directory, c.Ref.Path, 80<<10)
			if err == nil && hash(actual) != hash(c.After) {
				return nil, conflict()
			}
			if err != nil {
				return nil, err
			}
		}
		if c.BeforeExists {
			var m Memory
			m.Ref = c.Ref
			m.OwnerID = owner
			if err = decode(c.Before, &m); err != nil {
				return nil, err
			}
			suppressed, err := f.Suppressed(ctx, m)
			if err != nil {
				return nil, err
			}
			if suppressed {
				retained = append(retained, c.Ref)
				continue
			}
			if err = textfiles.MkdirAll(root.Directory, path.Dir(c.Ref.Path)); err != nil {
				return nil, err
			}
			_, err = textfiles.Replace(ctx, root.Directory, c.Ref.Path, string(c.Before), c.AfterExists, stat, check)
		} else {
			err = textfiles.Remove(ctx, root.Directory, c.Ref.Path, stat, check)
		}
		if err != nil {
			return nil, err
		}
	}
	r.Undone = true
	data, _ := json.Marshal(r)
	_, err = textfiles.Replace(ctx, dir, name, string(data), true, journalStat, check)
	return retained, err
}

// Forgetting must also destroy tool-operation recovery copies, including moved
// copies of the same content, so undo cannot bring a forgotten file back.
func (f *Files) forgetFileOperations(ctx context.Context, m Memory, check func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, _ := f.fileJournalPath(m.OwnerID, "")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "dream-file-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, stat, _, err := readRaw(dir, entry.Name(), 512<<10)
		if err != nil {
			return err
		}
		var record fileJournal
		if json.Unmarshal(raw, &record) != nil {
			return invalid("file_journal")
		}
		affected := false
		for _, a := range record.Sources {
			for _, b := range m.Sources {
				if a == b {
					affected = true
				}
			}
		}
		for _, change := range record.Changes {
			if change.Ref == m.Ref {
				affected = true
			}
			for _, data := range [][]byte{change.Before, change.After} {
				if len(data) == 0 {
					continue
				}
				var prior Memory
				if decode(data, &prior) == nil && digest(prior.Text) == digest(m.Text) {
					affected = true
				}
			}
		}
		if affected {
			if err = textfiles.Remove(ctx, dir, entry.Name(), stat, check); err != nil {
				return err
			}
		}
	}
	return nil
}

// Provenance is runtime bookkeeping kept outside editable Markdown. It records
// the input evidence of successful file operations, without asking the model
// to manufacture or classify source IDs.
func (f *Files) fileSources(ctx context.Context, owner core.ID, ref Ref, version string, initial []core.Object) ([]core.Object, bool, error) {
	out := append([]core.Object{}, initial...)
	seen := map[core.Object]bool{}
	for _, v := range out {
		seen[v] = true
	}
	dir, _ := f.fileJournalPath(owner, "")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return out, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	generated := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if !strings.HasPrefix(entry.Name(), "dream-file-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, _, _, err := readRaw(dir, entry.Name(), 512<<10)
		if core.CodeOf(err) == core.NotFound {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		var record fileJournal
		if json.Unmarshal(raw, &record) != nil {
			return nil, false, invalid("file_journal")
		}
		if record.Undone {
			continue
		}
		matches := false
		for _, change := range record.Changes {
			if change.Ref == ref && change.AfterExists && hash(change.After) == version {
				matches = true
			}
		}
		if !matches {
			continue
		}
		generated = true
		for _, source := range record.Sources {
			if !seen[source] {
				out = append(out, source)
				seen[source] = true
			}
		}
	}
	return out, generated, nil
}
