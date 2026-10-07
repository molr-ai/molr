// Package memory manages local Markdown files, identified by scope and path.
package memory

import (
	"context"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
)

type Scope string

const (
	Assistant Scope = "assistant"
	Global    Scope = "global"
)

type State string

const (
	Active     State = "active"
	Pending    State = "pending"
	Superseded State = "superseded"
)

// Ref is a file location, not a durable identity across renames. Owner comes
// exclusively from the authenticated principal. Paths are relative to /work or /global.
type Ref struct {
	Scope       Scope   `yaml:"scope"`
	AssistantID core.ID `yaml:"assistant_id,omitempty"`
	Path        string  `yaml:"path"`
}

func (r Ref) Validate(directory bool) error {
	if r.Scope != Assistant && r.Scope != Global || r.Scope == Assistant && r.AssistantID.Validate() != nil || r.Scope == Global && r.AssistantID != "" {
		return core.Invalid("scope", "assistant or global scope required")
	}
	if len(r.Path) > 1024 || !utf8.ValidString(r.Path) || path.Clean(r.Path) != r.Path || strings.ContainsAny(r.Path, "\\\x00") || strings.HasPrefix(r.Path, "/") {
		return core.Invalid("path", "canonical relative path required")
	}
	for i, part := range strings.Split(r.Path, "/") {
		if strings.TrimSpace(part) != part || strings.IndexFunc(part, unicode.IsControl) >= 0 || part == "" || part == "." || part == ".." || len(part) > 240 || strings.HasPrefix(part, ".") && (i != 0 || part != ".agents") {
			return core.Invalid("path", "ordinary semantic path required")
		}
	}
	if directory {
		if r.Path == ".agents/memory" || strings.HasPrefix(r.Path, ".agents/memory/") {
			return nil
		}
	} else if r.Path == "MEMORY.md" || strings.HasPrefix(r.Path, ".agents/memory/") && strings.HasSuffix(r.Path, ".md") {
		return nil
	}
	return core.Invalid("path", "use MEMORY.md or .agents/memory paths")
}
func (r Ref) LogicalPath() string {
	if r.Scope == Global {
		return "/global/" + r.Path
	}
	return "/work/" + r.Path
}

type Memory struct {
	Ref
	OwnerID              core.ID
	Summary              bool
	State                State
	Confirmed            bool
	Context, Evidence    string
	DreamID              core.ID
	Replaces             *Ref
	ReplacesVersion      string
	Text                 string
	Sources              []core.Object
	Version              string
	CreatedAt, UpdatedAt time.Time
}
type Query struct {
	State       State
	Scope       Scope
	AssistantID core.ID
	Text        string
	Page        core.PageRequest
}
type CreateRequest struct {
	Ref
	Context  string
	Mutation Mutation
	Text     string
	Sources  []core.Object
}
type UpdateRequest struct {
	Ref
	ReplacesVersion *string
	Context         *string
	State           *State
	Mutation        Mutation
	Text            string
	Sources         []core.Object
}
type DeleteRequest struct {
	Ref
	Mutation Mutation
	Forget   bool
}
type MoveRequest struct {
	Ref
	ToPath    string
	Directory bool
	Mutation  Mutation
}
type DirectoryRequest struct {
	Ref
	Mutation Mutation
}
type Entry struct {
	Ref
	Kind      string
	Version   string
	State     State `json:",omitempty"`
	UpdatedAt time.Time
}
type Directory struct {
	Ref
	Version string
	Entries []Entry
}
type Service interface {
	Create(context.Context, core.Principal, CreateRequest) (Memory, error)
	Get(context.Context, core.Principal, Ref) (Memory, error)
	List(context.Context, core.Principal, Query) (core.Page[Memory], error)
	Update(context.Context, core.Principal, UpdateRequest) (Memory, error)
	Delete(context.Context, core.Principal, DeleteRequest) error
	Browse(context.Context, core.Principal, Ref) (Directory, error)
	Mkdir(context.Context, core.Principal, DirectoryRequest) error
	Move(context.Context, core.Principal, MoveRequest) error
	RemoveDirectory(context.Context, core.Principal, DirectoryRequest) error
}
type Mutation struct {
	IdempotencyKey  string
	ExpectedVersion string
}

func (m Mutation) Validate(update bool) error {
	if err := (core.Mutation{IdempotencyKey: m.IdempotencyKey}).Validate(false); err != nil {
		return err
	}
	if update && len(m.ExpectedVersion) != 64 {
		return core.Invalid("expected_version", "file version required")
	}
	return nil
}
