package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

type reviewHeader struct {
	Sources []core.Object `yaml:"sources"`
}
type reviewReceipt struct{ Path, Version string }

// WriteReview creates a semantic, per-batch review file. The receipt contains
// only its path and content hash; replay never rewrites a deleted or edited file.
func (f *Files) WriteReview(ctx context.Context, owner, assistant, operation core.ID, name, summary string, sources []core.Object, check func() error) (string, error) {
	if path.Clean(name) != name || !strings.HasPrefix(name, ".agents/reviews/") || !strings.HasSuffix(name, ".md") || strings.ContainsAny(name, "\x00\\") {
		return "", invalid("review_path")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.root(ctx, Memory{Ref: Ref{Scope: Assistant, AssistantID: assistant}, OwnerID: owner})
	if err != nil {
		return "", err
	}
	h, err := yaml.Marshal(reviewHeader{sources})
	if err != nil {
		return "", err
	}
	raw := []byte("---\n" + string(h) + "---\n" + summary + "\n")
	if len(raw) > 80<<10 {
		return "", invalid("review")
	}
	version := hash(raw)
	journal := filepath.Join(f.options.JournalRoot, hash([]byte(owner)))
	if err = os.MkdirAll(journal, 0700); err != nil {
		return "", err
	}
	key := "review-" + hash([]byte(operation)) + ".json"
	if b, _, _, e := readRaw(journal, key, 4096); e == nil {
		var prior reviewReceipt
		if json.Unmarshal(b, &prior) != nil || prior.Path != name || prior.Version != version {
			return "", conflict()
		}
		current, _, _, e := readRaw(r.Directory, name, 80<<10)
		if e != nil || hash(current) != version {
			return "", conflict()
		}
		return version, nil
	} else if core.CodeOf(e) != core.NotFound {
		return "", e
	}
	if err = os.MkdirAll(r.Directory, 0700); err != nil {
		return "", err
	}
	if err = textfiles.MkdirAll(r.Directory, path.Dir(name)); err != nil {
		return "", err
	}
	b, _ := json.Marshal(reviewReceipt{name, version})
	if _, err = textfiles.Replace(ctx, journal, key, string(b), false, "", check); err != nil {
		return "", err
	}
	if _, err = textfiles.Replace(ctx, r.Directory, name, string(raw), false, "", check); err != nil {
		return "", err
	}
	return version, nil
}
func (f *Files) RemoveReview(ctx context.Context, owner, assistant core.ID, name, expected string, check func() error) (bool, error) {
	if !strings.HasPrefix(name, ".agents/reviews/") || path.Clean(name) != name {
		return false, invalid("review_path")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.root(ctx, Memory{Ref: Ref{Scope: Assistant, AssistantID: assistant}, OwnerID: owner})
	if err != nil {
		return false, err
	}
	data, stat, _, err := readRaw(r.Directory, name, 80<<10)
	if core.CodeOf(err) == core.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if hash(data) != expected {
		return true, nil
	}
	return false, textfiles.Remove(ctx, r.Directory, name, stat, check)
}
func (f *Files) forgetReviews(ctx context.Context, m Memory, check func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	roots, err := f.roots(ctx, m.OwnerID, RootQuery{})
	if err != nil {
		return err
	}
	budget := 4096
	for _, r := range roots {
		if r.Scope != Assistant || m.AssistantID != "" && r.AssistantID != m.AssistantID {
			continue
		}
		err = walk(ctx, r.Directory, ".agents/reviews", 0, &budget, func(name string) error {
			raw, stat, _, err := readRaw(r.Directory, name, 80<<10)
			if err != nil {
				return err
			}
			if !bytes.HasPrefix(raw, []byte("---\n")) {
				return nil
			}
			end := bytes.Index(raw[4:], []byte("\n---\n"))
			if end < 0 {
				return nil
			}
			var header reviewHeader
			if yaml.Unmarshal(raw[4:4+end], &header) != nil {
				return nil
			}
			for _, a := range header.Sources {
				for _, b := range m.Sources {
					if a == b {
						return textfiles.Remove(ctx, r.Directory, name, stat, check)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *service) scrub(ctx context.Context, p core.Principal, m Memory) error {
	if err := s.repo.forgetDerived(ctx, m, func() error { return s.access(ctx, p, m.Ref, true, false) }); err != nil {
		return err
	}

	if err := s.repo.forgetFileOperations(ctx, m, func() error { return s.access(ctx, p, m.Ref, true, false) }); err != nil {
		return err
	}
	if err := s.repo.forgetReviews(ctx, m, func() error { return s.access(ctx, p, m.Ref, true, false) }); err != nil {
		return err
	}
	return scrubHistory(ctx, s.store, m)
}

// Generated core cognition must not retain an explicitly forgotten topic.
func (f *Files) forgetDerived(ctx context.Context, m Memory, check func() error) error {
	roots, err := f.roots(ctx, m.OwnerID, RootQuery{})
	if err != nil {
		return err
	}
	for _, root := range roots {
		if root.Scope != Assistant || (m.Scope == Assistant && root.AssistantID != m.AssistantID) {
			continue
		}
		ref := Ref{Scope: root.Scope, AssistantID: root.AssistantID, Path: "MEMORY.md"}
		current, err := f.Get(ctx, m.OwnerID, ref)
		if core.CodeOf(err) == core.NotFound {
			continue
		}
		if err != nil {
			return err
		}
		sources, generated, err := f.fileSources(ctx, m.OwnerID, ref, current.Version, current.Sources)
		if err != nil {
			return err
		}
		current.Sources = sources
		if current.Confirmed || (current.DreamID == "" && !generated) {
			continue
		}
		affected := false
		for _, dep := range current.Dependencies {
			if dep.Ref == m.Ref {
				affected = true
			}
		}
		for _, a := range current.Sources {
			for _, b := range m.Sources {
				if a == b {
					affected = true
				}
			}
		}
		if affected {
			if err = f.Delete(ctx, current, current.Version, false, check); err != nil {
				return err
			}
		}
	}
	return nil
}
