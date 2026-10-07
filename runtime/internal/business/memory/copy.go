package memory

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

// Export streams current Markdown and forgetting records, without making a DB
// mirror. Each file is checked for concurrent edits; the archive is not a claim
// of a cross-file atomic snapshot.
func (f *Files) Export(ctx context.Context, owner core.ID, write func(string, []byte) error) error {
	roots, err := f.roots(ctx, owner)
	if err != nil {
		return err
	}
	budget := 4096
	total := 0
	for _, r := range roots {
		prefix := "global/"
		if r.Scope == Assistant {
			prefix = "assistants/" + string(r.AssistantID) + "/"
		}
		visit := func(name string) error {
			b, _, _, err := readRaw(r.Directory, name, 80<<10)
			if core.CodeOf(err) == core.NotFound {
				return nil
			}
			if err != nil {
				return err
			}
			total += len(b)
			if total > 16<<20 {
				return core.NewError(core.FailedPrecondition, "memory export exceeds limit")
			}
			return write(prefix+name, b)
		}
		if err = visit("MEMORY.md"); err != nil {
			return err
		}
		if err = walk(ctx, r.Directory, ".agents/reviews", 0, &budget, visit); err != nil {
			return err
		}
		if err = walk(ctx, r.Directory, ".agents/memory", 0, &budget, visit); err != nil {
			return err
		}
		dir, err := textfiles.OpenDirectory(r.Directory, ".agents/memory/.forgotten")
		if core.CodeOf(err) == core.NotFound {
			continue
		}
		if err != nil {
			return err
		}
		entries, err := dir.ReadDir(budget + 1)
		dir.Close()
		if err != nil && err != io.EOF {
			return err
		}
		for _, e := range entries {
			budget--
			if budget < 0 {
				return core.NewError(core.FailedPrecondition, "memory export entry limit")
			}
			if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".json") {
				if err = visit(".agents/memory/.forgotten/" + e.Name()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Clone keeps file paths and clear provenance, and never overwrites a target.
// Completion receipts live outside agent workspaces so replay does not recreate
// files the cloned assistant subsequently removed.
func (f *Files) Clone(ctx context.Context, p core.Principal, source, target core.ID) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if source == target {
		return invalid("assistant")
	}
	journal := filepath.Join(f.options.JournalRoot, "clones", hash([]byte(string(p.OwnerID)+"\x00"+string(source)+"\x00"+string(target))))
	if err := os.MkdirAll(journal, 0700); err != nil {
		return err
	}
	type entry struct {
		Source, Target               Ref
		SourceVersion, TargetVersion string
	}
	var plan []entry
	data, _, _, err := readRaw(journal, "plan.json", 1<<20)
	if err == nil {
		if json.Unmarshal(data, &plan) != nil {
			return invalid("clone_plan")
		}
	} else if core.CodeOf(err) == core.NotFound {
		items, err := f.all(ctx, p.OwnerID, Query{Scope: Assistant, AssistantID: source})
		if err != nil {
			return err
		}
		for _, m := range items {
			sourceRef, sourceVersion := m.Ref, m.Version
			m.AssistantID = target
			m.DreamID = ""
			m.Replaces = nil
			m.ReplacesVersion = ""
			m.Sources = nil
			m, err = f.Preview(ctx, m)
			if err != nil {
				return err
			}
			plan = append(plan, entry{sourceRef, m.Ref, sourceVersion, m.Version})
		}
		data, _ = json.Marshal(plan)
		if _, err = textfiles.Replace(ctx, journal, "plan.json", string(data), false, "", func() error { return ctx.Err() }); err != nil {
			return err
		}
	} else {
		return err
	}
	for _, item := range plan {
		sourceBytes, _ := json.Marshal(item.Source)
		key := hash(sourceBytes) + ".json"
		stat := ""
		started := false
		if b, v, _, e := readRaw(journal, key, 4096); e == nil {
			var done bool
			if json.Unmarshal(b, &done) != nil {
				return invalid("clone_receipt")
			}
			if done {
				continue
			}
			started = true
			stat = v
		} else if core.CodeOf(e) != core.NotFound {
			return e
		}
		current, e := f.Get(ctx, p.OwnerID, item.Target)
		if e == nil {
			if current.Version != item.TargetVersion {
				return conflict()
			}
		} else if core.CodeOf(e) == core.NotFound {
			if started {
				return core.NewError(core.Conflict, "partially copied memory is missing; review destination")
			}
			m, e := f.Get(ctx, p.OwnerID, item.Source)
			if e != nil {
				return e
			}
			if m.Version != item.SourceVersion {
				return conflict()
			}
			m.AssistantID = target
			m.DreamID = ""
			m.Replaces = nil
			m.ReplacesVersion = ""
			m.Sources = nil
			m, e = f.Preview(ctx, m)
			if e != nil {
				return e
			}
			if m.Version != item.TargetVersion {
				return conflict()
			}
			stat, e = textfiles.Replace(ctx, journal, key, "false", false, "", func() error { return ctx.Err() })
			if e != nil {
				return e
			}
			if _, e = f.Put(ctx, m, "", nil); e != nil {
				return e
			}
		} else {
			return e
		}
		if _, err = textfiles.Replace(ctx, journal, key, "true", stat != "", stat, func() error { return ctx.Err() }); err != nil {
			return err
		}
	}

	// Forgetting follows the source: copy only hash/source-reference tombstones.
	roots, err := f.roots(ctx, p.OwnerID)
	if err != nil {
		return err
	}
	var from, to Root
	for _, r := range roots {
		if r.Scope == Assistant && r.AssistantID == source {
			from = r
		}
		if r.Scope == Assistant && r.AssistantID == target {
			to = r
		}
	}
	if from.Directory == "" || to.Directory == "" {
		return missing()
	}
	dir, err := textfiles.OpenDirectory(from.Directory, ".agents/memory/.forgotten")
	if core.CodeOf(err) == core.NotFound {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(4097)
	dir.Close()
	if err != nil && err != io.EOF {
		return err
	}
	if len(entries) > 4096 {
		return core.NewError(core.FailedPrecondition, "forget copy limit")
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := ".agents/memory/.forgotten/" + e.Name()
		b, _, _, err := readRaw(from.Directory, name, 80<<10)
		if err != nil {
			return err
		}
		var t Tombstone
		if json.Unmarshal(b, &t) != nil {
			return invalid("tombstone")
		}
		t.AssistantID = target
		b, _ = json.Marshal(t)
		if err = os.MkdirAll(to.Directory, 0700); err != nil {
			return err
		}
		if err = textfiles.MkdirAll(to.Directory, path.Dir(name)); err != nil {
			return err
		}
		if _, _, _, err = readRaw(to.Directory, name, 80<<10); err == nil {
			continue
		} else if core.CodeOf(err) != core.NotFound {
			return err
		}
		if _, err = textfiles.Replace(ctx, to.Directory, name, string(b), false, "", func() error { return ctx.Err() }); err != nil {
			return err
		}
	}
	return nil
}
