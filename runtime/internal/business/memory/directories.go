package memory

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

func (s *service) Browse(ctx context.Context, p core.Principal, ref Ref) (Directory, error) {
	out := Directory{Ref: ref, Entries: []Entry{}}
	if err := s.access(ctx, p, ref, false, true); err != nil {
		return out, err
	}
	r, err := s.repo.root(ctx, Memory{Ref: ref, OwnerID: p.OwnerID})
	if err != nil {
		return out, err
	}
	d, err := textfiles.OpenDirectory(r.Directory, ref.Path)
	if core.CodeOf(err) == core.NotFound {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	defer d.Close()
	out.Version, err = textfiles.Version(d)
	if err != nil {
		return out, err
	}
	rows, err := d.ReadDir(4097)
	if err != nil && err != io.EOF {
		return out, err
	}
	if len(rows) > 4096 {
		return out, core.NewError(core.FailedPrecondition, "directory entry limit")
	}
	for _, e := range rows {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		child := ref
		child.Path = path.Join(ref.Path, e.Name())
		item := Entry{Ref: child}
		if e.IsDir() {
			file, err := textfiles.OpenDirectory(r.Directory, child.Path)
			if err != nil {
				return out, err
			}
			item.Kind = "directory"
			item.Version, err = textfiles.Version(file)
			file.Close()
			if err != nil {
				return out, err
			}
		} else if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".md") {
			m, _, err := s.repo.read(ctx, p.OwnerID, r, child.Path)
			if err != nil {
				return out, err
			}
			item.Kind = "file"
			item.Version = m.Version
			item.State = m.State
			item.UpdatedAt = m.UpdatedAt
		} else {
			continue
		}
		out.Entries = append(out.Entries, item)
	}
	current, err := textfiles.Version(d)
	if err != nil {
		return out, err
	}
	if current != out.Version {
		return out, textfiles.Changed()
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.Kind != b.Kind {
			return a.Kind == "directory"
		}
		return a.Path < b.Path
	})
	return out, nil
}
func (s *service) Mkdir(ctx context.Context, p core.Principal, req DirectoryRequest) error {
	if err := req.Mutation.Validate(false); err != nil {
		return err
	}
	if err := s.access(ctx, p, req.Ref, true, true); err != nil {
		return err
	}
	r, err := s.repo.root(ctx, Memory{Ref: req.Ref, OwnerID: p.OwnerID})
	if err != nil {
		return err
	}
	if err = os.MkdirAll(r.Directory, 0700); err != nil {
		return err
	}
	return textfiles.MkdirAll(r.Directory, req.Path)
}

// Path operation receipts contain only locations and versions. Uncertain moves
// with a missing source fail conservatively instead of following guessed identities.
type pathReceipt struct {
	Digest, Version string
	Done            bool
}

func (s *service) pathMutation(ctx context.Context, p core.Principal, kind string, req MoveRequest) error {
	if err := req.Mutation.Validate(true); err != nil {
		return err
	}
	if err := s.access(ctx, p, req.Ref, true, req.Directory); err != nil {
		return err
	}
	if req.Path == ".agents/memory" || req.Path == "MEMORY.md" {
		return core.Invalid("path", "reserved root cannot be moved or removed as a directory")
	}
	dest := req.Ref
	dest.Path = req.ToPath
	if kind == "move" {
		if err := s.access(ctx, p, dest, true, req.Directory); err != nil {
			return err
		}
	}
	if s.repo == nil || s.repo.options.JournalRoot == "" {
		return core.NewError(core.Unavailable, "memory journal unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	root, err := s.repo.root(ctx, Memory{Ref: req.Ref, OwnerID: p.OwnerID})
	if err != nil {
		return err
	}
	journal := filepath.Join(s.repo.options.JournalRoot, hash([]byte(p.OwnerID)))
	if err = os.MkdirAll(journal, 0700); err != nil {
		return err
	}
	body, _ := json.Marshal(req)
	refBytes, _ := json.Marshal(req.Ref)
	name := hash([]byte(kind+"\x00"+string(refBytes)+"\x00"+req.Mutation.IdempotencyKey)) + ".json"
	record := pathReceipt{Digest: hash(body)}
	stat := ""
	if data, v, _, e := readRaw(journal, name, 4096); e == nil {
		if json.Unmarshal(data, &record) != nil {
			return invalid("receipt")
		}
		if record.Digest != hash(body) {
			return conflict()
		}
		stat = v
		if record.Done {
			return nil
		}
	} else if core.CodeOf(e) != core.NotFound {
		return e
	}
	version := req.Mutation.ExpectedVersion
	if !req.Directory {
		m, v, e := s.repo.read(ctx, p.OwnerID, root, req.Path)
		if e != nil {
			return e
		}
		if m.Version != version {
			return conflict()
		}
		version = v
	}
	data, _ := json.Marshal(record)
	stat, err = textfiles.Replace(ctx, journal, name, string(data), stat != "", stat, func() error { return ctx.Err() })
	if err != nil {
		return err
	}
	check := func() error {
		if err := s.access(ctx, p, req.Ref, true, req.Directory); err != nil {
			return err
		}
		if kind == "move" {
			return s.access(ctx, p, dest, true, req.Directory)
		}
		return nil
	}
	if kind == "move" {
		record.Version, err = textfiles.Move(ctx, root.Directory, req.Path, req.ToPath, version, req.Directory, check)
	} else {
		err = textfiles.RemoveDirectory(ctx, root.Directory, req.Path, version, check)
	}
	if err != nil {
		return err
	}
	record.Done = true
	data, _ = json.Marshal(record)
	_, err = textfiles.Replace(ctx, journal, name, string(data), true, stat, func() error { return ctx.Err() })
	return err
}
func (s *service) Move(ctx context.Context, p core.Principal, req MoveRequest) error {
	return s.pathMutation(ctx, p, "move", req)
}
func (s *service) RemoveDirectory(ctx context.Context, p core.Principal, req DirectoryRequest) error {
	return s.pathMutation(ctx, p, "rmdir", MoveRequest{Ref: req.Ref, Mutation: req.Mutation, Directory: true})
}
