package memory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

// Roots are supplied by the Runtime from authenticated ownership, never by a
// client or frontmatter. Remote computers cannot be memory roots.
type Root struct {
	Scope       Scope
	AssistantID core.ID
	Directory   string
}

// RootQuery narrows directory discovery; an empty query enumerates every root.
// Paths stay in Ref and never participate in resolving trusted root directories.
type RootQuery struct {
	Scope       Scope
	AssistantID core.ID
}
type FileOptions struct {
	Roots func(context.Context, core.ID, RootQuery) ([]Root, error)
	// JournalRoot is private Runtime state, outside agent-writable workspaces.
	JournalRoot  string
	BeforeCommit func() error
}
type Files struct {
	options FileOptions
	mu      sync.Mutex
}

func NewFiles(o FileOptions) *Files { return &Files{options: o} }
func hash(b []byte) string          { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func memoryName(p string) (string, error) {
	if err := (Ref{Scope: Global, Path: p}).Validate(false); err != nil {
		return "", err
	}
	return p, nil
}
func (f *Files) roots(ctx context.Context, owner core.ID, q RootQuery) ([]Root, error) {
	if owner.Validate() != nil || f == nil || f.options.Roots == nil {
		return nil, core.NewError(core.Unavailable, "memory roots unavailable")
	}
	return f.options.Roots(ctx, owner, q)
}
func (f *Files) root(ctx context.Context, m Memory) (Root, error) {
	roots, err := f.roots(ctx, m.OwnerID, RootQuery{Scope: m.Scope, AssistantID: m.AssistantID})
	if err != nil {
		return Root{}, err
	}
	for _, r := range roots {
		if r.Scope == m.Scope && r.AssistantID == m.AssistantID {
			return r, nil
		}
	}
	return Root{}, missing()
}

type metadata struct {
	State           State         `yaml:"state,omitempty"`
	Confirmed       bool          `yaml:"confirmed,omitempty"`
	Context         string        `yaml:"context,omitempty"`
	Evidence        string        `yaml:"evidence,omitempty"`
	DreamID         core.ID       `yaml:"dream_id,omitempty"`
	Replaces        *Ref          `yaml:"replaces,omitempty"`
	ReplacesVersion string        `yaml:"replaces_version,omitempty"`
	Sources         []core.Object `yaml:"sources,omitempty"`
	Dependencies    []Dependency  `yaml:"dependencies,omitempty"`
	CreatedAt       time.Time     `yaml:"created_at,omitempty"`
	UpdatedAt       time.Time     `yaml:"updated_at,omitempty"`
}

func decode(data []byte, m *Memory) error {
	if !utf8.Valid(data) {
		return invalid("text")
	}
	body := data
	var h metadata
	if bytes.HasPrefix(data, []byte("---\n")) {
		end := bytes.Index(data[4:], []byte("\n---\n"))
		if end < 0 {
			return invalid("frontmatter")
		}
		d := yaml.NewDecoder(bytes.NewReader(data[4 : 4+end]))
		d.KnownFields(true)
		if err := d.Decode(&h); err != nil {
			return invalid("frontmatter")
		}
		body = data[4+end+5:]
	}
	if len(body) > 65536 || !validText(string(body), 65536) {
		return invalid("text")
	}
	if err := validateMetadata(h.Context); err != nil {
		return err
	}
	if h.State != "" && h.State != Active && h.State != Pending && h.State != Superseded {
		return invalid("state")
	}
	if len(h.Dependencies) > 40 {
		return invalid("dependencies")
	}
	for _, d := range h.Dependencies {
		if d.Ref.Validate(false) != nil || d.Ref.Path == "MEMORY.md" || len(d.Version) != 64 {
			return invalid("dependencies")
		}
	}
	m.Text = string(body)
	m.State = state(h.State)
	m.Confirmed = h.Confirmed
	m.Context = h.Context
	m.Evidence = h.Evidence
	m.DreamID = h.DreamID
	m.Replaces = h.Replaces
	m.ReplacesVersion = h.ReplacesVersion
	m.Sources = h.Sources
	m.Dependencies = h.Dependencies
	if !h.CreatedAt.IsZero() {
		m.CreatedAt = h.CreatedAt
	}
	if !h.UpdatedAt.IsZero() && h.UpdatedAt.After(m.UpdatedAt) {
		m.UpdatedAt = h.UpdatedAt
	}
	return nil
}
func encode(m Memory) ([]byte, error) {
	h := metadata{State: m.State, Confirmed: m.Confirmed, Context: m.Context, Evidence: m.Evidence, DreamID: m.DreamID, Replaces: m.Replaces, ReplacesVersion: m.ReplacesVersion, Sources: m.Sources, Dependencies: m.Dependencies, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	data, err := yaml.Marshal(h)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(data) + "---\n" + m.Text), nil
}
func readRaw(root, name string, limit int64) ([]byte, string, time.Time, error) {
	file, err := textfiles.Open(root, name)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	defer file.Close()
	before, err := textfiles.Version(file)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	st, err := file.Stat()
	if err != nil {
		return nil, "", time.Time{}, err
	}
	if st.Size() > limit {
		return nil, "", time.Time{}, core.NewError(core.FailedPrecondition, "memory file exceeds limit")
	}
	b, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, "", time.Time{}, err
	}
	after, err := textfiles.Version(file)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	if before != after || int64(len(b)) > limit {
		return nil, "", time.Time{}, textfiles.Changed()
	}
	return b, before, st.ModTime(), nil
}
func (f *Files) read(ctx context.Context, owner core.ID, r Root, name string) (Memory, string, error) {
	if err := ctx.Err(); err != nil {
		return Memory{}, "", err
	}
	b, stat, at, err := readRaw(r.Directory, name, 80<<10)
	if err != nil {
		return Memory{}, "", err
	}
	m := Memory{Ref: Ref{Scope: r.Scope, AssistantID: r.AssistantID, Path: name}, OwnerID: owner, Summary: name == "MEMORY.md", Version: hash(b), CreatedAt: at, UpdatedAt: at}
	err = decode(b, &m)
	return m, stat, err
}

// walk opens each directory through a pinned, no-link descriptor. Limits apply
// across all roots for an owner; neither symlinks nor special files are followed.
func walk(ctx context.Context, root, name string, depth int, budget *int, visit func(string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 16 {
		return core.NewError(core.FailedPrecondition, "memory directory depth exceeded")
	}
	dir, err := textfiles.OpenDirectory(root, name)
	if core.CodeOf(err) == core.NotFound {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(*budget + 1)
	dir.Close()
	if err != nil && err != io.EOF {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		*budget--
		if *budget < 0 {
			return core.NewError(core.FailedPrecondition, "memory scan limit exceeded")
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		child := path.Join(name, e.Name())
		if e.IsDir() {
			if err = walk(ctx, root, child, depth+1, budget, visit); err != nil {
				return err
			}
		} else if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".md") {
			if err = visit(child); err != nil {
				return err
			}
		}
	}
	return nil
}
func (f *Files) all(ctx context.Context, owner core.ID, q Query) ([]Memory, error) {
	roots, err := f.roots(ctx, owner, RootQuery{Scope: q.Scope, AssistantID: q.AssistantID})
	if err != nil {
		return nil, err
	}
	items := []Memory{}
	budget := 4096
	total := 0
	for _, r := range roots {
		if q.Scope != "" && r.Scope != q.Scope || q.AssistantID != "" && r.AssistantID != q.AssistantID {
			continue
		}
		visit := func(name string) error {
			m, _, err := f.read(ctx, owner, r, name)
			if core.CodeOf(err) == core.NotFound {
				return nil
			}
			if err != nil {
				return err
			}
			total += len(m.Text)
			if total > 8<<20 {
				return core.NewError(core.FailedPrecondition, "memory content scan limit exceeded")
			}
			if (q.State == "" || m.State == q.State) && (q.Text == "" || strings.Contains(strings.ToLower(m.Text), strings.ToLower(q.Text))) {
				items = append(items, m)
			}
			return nil
		}
		if err = visit("MEMORY.md"); err != nil {
			return nil, err
		}
		if err = walk(ctx, r.Directory, ".agents/memory", 0, &budget, visit); err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Scope < items[j].Scope || items[i].Scope == items[j].Scope && (items[i].AssistantID < items[j].AssistantID || items[i].AssistantID == items[j].AssistantID && items[i].Path < items[j].Path)
	})
	return items, nil
}
func (f *Files) Get(ctx context.Context, owner core.ID, ref Ref) (Memory, error) {
	if err := ref.Validate(false); err != nil {
		return Memory{}, err
	}
	r, err := f.root(ctx, Memory{Ref: ref, OwnerID: owner})
	if err != nil {
		return Memory{}, err
	}
	m, _, err := f.read(ctx, owner, r, ref.Path)
	return m, err
}

// Put commits exactly one file. check runs immediately before its atomic rename.
// No filesystem write participates in a database transaction.
func (f *Files) Put(ctx context.Context, m Memory, expected string, check func() error) (Memory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.put(ctx, m, expected, check)
}
func (f *Files) put(ctx context.Context, m Memory, expected string, check func() error) (Memory, error) {
	if !validText(m.Text, 65536) || !utf8.ValidString(m.Text) || len(m.Sources) > 256 || len(m.Dependencies) > 40 {
		return m, invalid("memory")
	}
	if err := validateMetadata(m.Context); err != nil {
		return m, err
	}
	r, err := f.root(ctx, m)
	if err != nil {
		return m, err
	}
	name, err := memoryName(m.Path)
	if err != nil {
		return m, err
	}
	if err = os.MkdirAll(r.Directory, 0700); err != nil {
		return m, err
	}
	if err = textfiles.MkdirAll(r.Directory, path.Dir(name)); err != nil {
		return m, err
	}
	stat := ""
	if expected != "" {
		old, s, err := f.read(ctx, m.OwnerID, r, name)
		if err != nil {
			return m, err
		}
		if old.Version != expected {
			return m, conflict()
		}
		stat = s
	}
	data, err := encode(m)
	if err != nil {
		return m, err
	}
	if len(data) > 80<<10 {
		return m, invalid("memory")
	}
	m.Path = name
	m.Summary = name == "MEMORY.md"
	m.Version = hash(data)
	if err = planMutation(ctx, m); err != nil {
		return m, err
	}
	authorize := func() error {
		if f.options.BeforeCommit != nil {
			if err := f.options.BeforeCommit(); err != nil {
				return err
			}
		}
		if check != nil {
			return check()
		}
		return ctx.Err()
	}
	if _, err = textfiles.Replace(ctx, r.Directory, name, string(data), expected != "", stat, authorize); err != nil {
		return m, err
	}
	return m, nil
}
func (f *Files) Delete(ctx context.Context, m Memory, expected string, forget bool, check func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.root(ctx, m)
	if err != nil {
		return err
	}
	name, err := memoryName(m.Path)
	if err != nil {
		return err
	}
	current, stat, err := f.read(ctx, m.OwnerID, r, name)
	if err != nil {
		return err
	}
	if current.Version != expected {
		return conflict()
	}
	// Tombstone first: a crash may retain the current file, but cannot allow the
	// next dream to silently resurrect forgotten evidence.
	if forget {
		current.Sources = m.Sources
		if err = f.forget(ctx, r, current, check); err != nil {
			return err
		}
	}
	if err = planMutation(ctx, Memory{Ref: m.Ref, OwnerID: m.OwnerID, Sources: m.Sources}); err != nil {
		return err
	}
	return textfiles.Remove(ctx, r.Directory, name, stat, func() error {
		if check != nil {
			return check()
		}
		return ctx.Err()
	})
}
func (f *Files) forget(ctx context.Context, r Root, m Memory, check func() error) error {
	name := ".agents/memory/.forgotten/" + hash([]byte(m.Path+"\x00"+m.Version)) + ".json"
	b, _ := json.Marshal(Tombstone{m.OwnerID, m.AssistantID, digest(m.Text), m.Sources})
	if err := textfiles.MkdirAll(r.Directory, path.Dir(name)); err != nil {
		return err
	}
	if old, _, _, err := readRaw(r.Directory, name, 80<<10); err == nil {
		if bytes.Equal(old, b) {
			return nil
		}
		return conflict()
	} else if core.CodeOf(err) != core.NotFound {
		return err
	}
	_, err := textfiles.Replace(ctx, r.Directory, name, string(b), false, "", func() error {
		if check != nil {
			return check()
		}
		return ctx.Err()
	})
	return err
}
func (f *Files) Suppressed(ctx context.Context, m Memory) (bool, error) {
	roots, err := f.roots(ctx, m.OwnerID, RootQuery{})
	if err != nil {
		return false, err
	}
	budget := 4096
	for _, r := range roots {
		if r.Scope == Assistant && r.AssistantID != m.AssistantID {
			continue
		}
		dir, err := textfiles.OpenDirectory(r.Directory, ".agents/memory/.forgotten")
		if core.CodeOf(err) == core.NotFound {
			continue
		}
		if err != nil {
			return false, err
		}
		entries, err := dir.ReadDir(budget + 1)
		dir.Close()
		if err != nil && err != io.EOF {
			return false, err
		}
		for _, e := range entries {
			budget--
			if budget < 0 {
				return false, core.NewError(core.FailedPrecondition, "forget scan limit exceeded")
			}
			if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			b, _, _, err := readRaw(r.Directory, ".agents/memory/.forgotten/"+e.Name(), 80<<10)
			if err != nil {
				return false, err
			}
			var t Tombstone
			if json.Unmarshal(b, &t) != nil {
				return false, invalid("tombstone")
			}
			if t.Hash == digest(m.Text) {
				return true, nil
			}
			for _, a := range t.Sources {
				for _, b := range m.Sources {
					if a == b {
						return true, nil
					}
				}
			}
		}
	}
	return false, nil
}

// Preview determines a file reference and complete content revision without writing.
func (f *Files) Preview(ctx context.Context, m Memory) (Memory, error) {
	if err := m.Ref.Validate(false); err != nil {
		return m, err
	}
	if _, err := f.root(ctx, m); err != nil {
		return m, err
	}
	data, err := encode(m)
	if err != nil {
		return m, err
	}
	m.Version = hash(data)
	return m, nil
}
