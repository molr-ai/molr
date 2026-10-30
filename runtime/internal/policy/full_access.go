package policy

import (
	"molr.ai/runtime/internal/core"
	"path"
	"slices"
	"strings"
)

// FullAccessScope explicitly selects unrestricted access or a set of host programs.
// Missing scopes never grant access. Program rules constrain launch, not program behavior.
type FullAccessScope struct {
	Mode     string        `json:"mode"`
	Programs []ProgramRule `json:"programs,omitempty"`
}
type ProgramRule struct {
	Executable string   `json:"executable"`
	Arguments  string   `json:"arguments"`
	Args       []string `json:"args,omitempty"`
}
type ProgramInvocation struct {
	Executable string
	Args       []string
}

// ValidateProgramInvocation accepts absolute POSIX or Windows drive/UNC paths,
// independently of the controller OS. The execution node validates its native path.
func ValidateProgramInvocation(executable string, args []string) error {
	p := strings.ReplaceAll(executable, `\`, "/")
	absolute := strings.HasPrefix(p, "/") || len(p) > 3 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':' && p[2] == '/'
	if !absolute || len(p) > 4096 || strings.ContainsRune(p, 0) || strings.HasSuffix(p, "/") || strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") || strings.Contains(p, "/./") || strings.HasSuffix(p, "/.") || path.Base(p) == "." {
		return core.Invalid("executable", "absolute program path without dot segments required")
	}
	if len(args) > 256 {
		return core.Invalid("args", "at most 256 arguments")
	}
	size := 0
	for _, a := range args {
		size += len(a)
		if strings.ContainsRune(a, 0) {
			return core.Invalid("args", "NUL is not allowed")
		}
	}
	if size > 65536 {
		return core.Invalid("args", "arguments exceed 65536 bytes")
	}
	return nil
}
func (s *FullAccessScope) Validate() error {
	if s == nil {
		return core.Invalid("full_access_scope", "explicit Full Access scope required: unrestricted or programs")
	}
	switch s.Mode {
	case "unrestricted":
		if len(s.Programs) != 0 {
			return core.Invalid("full_access_scope", "unrestricted scope cannot contain program rules")
		}
	case "programs":
		if len(s.Programs) == 0 || len(s.Programs) > 32 {
			return core.Invalid("full_access_scope", "one to 32 program rules required")
		}
		for _, r := range s.Programs {
			if err := ValidateProgramInvocation(r.Executable, r.Args); err != nil {
				return err
			}
			if r.Arguments != "any" && r.Arguments != "exact" || r.Arguments == "any" && len(r.Args) != 0 {
				return core.Invalid("full_access_scope", "arguments must explicitly be any or exact; any cannot include args")
			}
		}
	default:
		return core.Invalid("full_access_scope", "select unrestricted or programs explicitly")
	}
	return nil
}
func (s *FullAccessScope) Allows(program *ProgramInvocation) bool {
	if s.Validate() != nil {
		return false
	}
	if s.Mode == "unrestricted" {
		return true
	}
	if program == nil || program.Executable == "" {
		return false
	}
	for _, r := range s.Programs {
		if r.Executable == program.Executable && (r.Arguments == "any" || slices.Equal(r.Args, program.Args)) {
			return true
		}
	}
	return false
}
func (s *FullAccessScope) Covers(want *FullAccessScope) bool {
	if s.Validate() != nil || want.Validate() != nil {
		return false
	}
	if s.Mode == "unrestricted" {
		return true
	}
	if want.Mode != "programs" {
		return false
	}
	for _, rule := range want.Programs {
		covered := false
		for _, have := range s.Programs {
			if have.Executable == rule.Executable && (have.Arguments == "any" || rule.Arguments == "exact" && slices.Equal(have.Args, rule.Args)) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}
