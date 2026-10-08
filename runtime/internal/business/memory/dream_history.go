package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

// Dream undo images are private recovery data, never another live memory store.
// Keeping original bytes permits a precise restore without rewriting user edits.
type dreamBefore struct {
	Ref    Ref
	Before []byte
	After  string
}

func (f *Files) dreamHistory(owner, operation core.ID, ref Ref) (string, string) {
	return filepath.Join(f.options.JournalRoot, hash([]byte(owner))), "dream-undo-" + hash([]byte(string(operation)+"\x00"+string(ref.AssistantID)+"\x00"+ref.Path)) + ".json"
}
func (f *Files) BackupDream(ctx context.Context, operation core.ID, next Memory, expected string, check func() error) error {
	if check == nil {
		check = func() error { return ctx.Err() }
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, name := f.dreamHistory(next.OwnerID, operation, next.Ref)
	if raw, _, _, err := readRaw(dir, name, 128<<10); err == nil {
		var prior dreamBefore
		if json.Unmarshal(raw, &prior) != nil || prior.Ref != next.Ref || prior.After != next.Version || hash(prior.Before) != expected {
			return conflict()
		}
		return nil
	} else if core.CodeOf(err) != core.NotFound {
		return err
	}
	root, err := f.root(ctx, next)
	if err != nil {
		return err
	}
	before, _, _, err := readRaw(root.Directory, next.Path, 80<<10)
	if err != nil {
		return err
	}
	if hash(before) != expected {
		return conflict()
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(dreamBefore{next.Ref, before, next.Version})
	if err != nil {
		return err
	}
	_, err = textfiles.Replace(ctx, dir, name, string(raw), false, "", check)
	return err
}

// RestoreDream returns false for later edits, confirmations or forgotten facts.
// Repeating an interrupted undo is harmless when the original bytes are present.
func (f *Files) RestoreDream(ctx context.Context, owner, operation core.ID, ref Ref, expected string, check func() error) (bool, error) {
	if check == nil {
		check = func() error { return ctx.Err() }
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, name := f.dreamHistory(owner, operation, ref)
	raw, _, _, err := readRaw(dir, name, 128<<10)
	if core.CodeOf(err) == core.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var saved dreamBefore
	if json.Unmarshal(raw, &saved) != nil || saved.Ref != ref || saved.After != expected {
		return false, conflict()
	}
	prior := Memory{Ref: ref, OwnerID: owner}
	if err = decode(saved.Before, &prior); err != nil {
		return false, err
	}
	blocked, err := f.Suppressed(ctx, prior)
	if err != nil || blocked {
		return false, err
	}
	root, err := f.root(ctx, prior)
	if err != nil {
		return false, err
	}
	current, stat, _, err := readRaw(root.Directory, ref.Path, 80<<10)
	if core.CodeOf(err) == core.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if hash(current) == hash(saved.Before) {
		return true, nil
	}
	if hash(current) != expected {
		return false, nil
	}
	m := Memory{}
	if err = decode(current, &m); err != nil {
		return false, err
	}
	if m.Confirmed {
		return false, nil
	}
	_, err = textfiles.Replace(ctx, root.Directory, ref.Path, string(saved.Before), true, stat, check)
	return err == nil, err
}

// Forgetting also removes recovery copies that could otherwise revive the fact.
func (f *Files) forgetDreamHistory(ctx context.Context, m Memory, check func() error) error {
	dir, _ := f.dreamHistory(m.OwnerID, "", m.Ref)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "dream-undo-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, stat, _, err := readRaw(dir, entry.Name(), 128<<10)
		if err != nil {
			return err
		}
		var saved dreamBefore
		if json.Unmarshal(raw, &saved) != nil {
			return invalid("dream_history")
		}
		prior := Memory{Ref: saved.Ref, OwnerID: m.OwnerID}
		if err = decode(saved.Before, &prior); err != nil {
			return err
		}
		affected := saved.Ref == m.Ref
		for _, a := range prior.Sources {
			for _, b := range m.Sources {
				if a == b {
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
