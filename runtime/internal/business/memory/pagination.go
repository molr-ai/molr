package memory

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"

	"molr.ai/runtime/internal/core"
)

type memoryCandidate struct {
	root Root
	ref  Ref
}

func compareMemoryRef(a, b Ref) int {
	if n := cmp.Compare(a.Scope, b.Scope); n != 0 {
		return n
	}
	if n := cmp.Compare(a.AssistantID, b.AssistantID); n != 0 {
		return n
	}
	return cmp.Compare(a.Path, b.Path)
}

// List enumerates bounded path metadata, then reads only the suffix needed for
// this page and one matching lookahead. Files remain authoritative; no contents
// are cached between requests. The cursor is a query-bound logical position.
func (f *Files) List(ctx context.Context, owner core.ID, q Query) (core.Page[Memory], error) {
	out := core.Page[Memory]{Items: []Memory{}}
	page, err := q.Page.Normalize()
	if err != nil {
		return out, err
	}
	q.Page = core.PageRequest{}
	raw, _ := json.Marshal(struct {
		Owner core.ID
		Query Query
	}{owner, q})
	scope := hash(raw)
	var after Ref
	if page.Cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(page.Cursor)
		fields := strings.Split(string(decoded), "\x00")
		if err != nil || len(fields) != 4 || fields[0] != scope {
			return out, invalid("cursor")
		}
		after = Ref{Scope: Scope(fields[1]), AssistantID: core.ID(fields[2]), Path: fields[3]}
		if after.Validate(false) != nil {
			return out, invalid("cursor")
		}
	}
	roots, err := f.roots(ctx, owner)
	if err != nil {
		return out, err
	}
	var candidates []memoryCandidate
	budget := 4096
	for _, root := range roots {
		if q.Scope != "" && root.Scope != q.Scope || q.AssistantID != "" && root.AssistantID != q.AssistantID {
			continue
		}
		add := func(name string) error {
			ref := Ref{Scope: root.Scope, AssistantID: root.AssistantID, Path: name}
			if err := ref.Validate(false); err != nil {
				return err
			}
			candidates = append(candidates, memoryCandidate{root, ref})
			return nil
		}
		if err := add("MEMORY.md"); err != nil {
			return out, err
		}
		if err := walk(ctx, root.Directory, ".agents/memory", 0, &budget, add); err != nil {
			return out, err
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return compareMemoryRef(candidates[i].ref, candidates[j].ref) < 0 })
	needle := strings.ToLower(q.Text)
	total := 0
	for _, candidate := range candidates {
		if page.Cursor != "" && compareMemoryRef(candidate.ref, after) <= 0 {
			continue
		}
		m, _, err := f.read(ctx, owner, candidate.root, candidate.ref.Path)
		if core.CodeOf(err) == core.NotFound {
			continue
		}
		if err != nil {
			return core.Page[Memory]{}, err
		}
		total += len(m.Text)
		if total > 8<<20 {
			return core.Page[Memory]{}, core.NewError(core.FailedPrecondition, "memory content scan limit exceeded")
		}
		if q.State != "" && m.State != q.State || needle != "" && !strings.Contains(strings.ToLower(m.Text), needle) {
			continue
		}
		if len(out.Items) == page.Limit {
			last := out.Items[len(out.Items)-1].Ref
			// NUL separates validated fields without JSON expansion of long Unicode or
			// HTML-looking filenames; valid paths fit PageRequest's cursor bound.
			out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(scope + "\x00" + string(last.Scope) + "\x00" + string(last.AssistantID) + "\x00" + last.Path))
			return out, nil
		}
		out.Items = append(out.Items, m)
	}
	return out, nil
}
