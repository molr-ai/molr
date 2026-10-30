package policy

import (
	"testing"
	"time"
)

func TestFullAccessScopeValidationAndMatching(t *testing.T) {
	exact := &FullAccessScope{Mode: "programs", Programs: []ProgramRule{{Executable: "/usr/bin/tool", Arguments: "exact", Args: []string{"a b", "", "$(touch sentinel)"}}}}
	any := &FullAccessScope{Mode: "programs", Programs: []ProgramRule{{Executable: "/usr/bin/tool", Arguments: "any"}}}
	all := &FullAccessScope{Mode: "unrestricted"}
	for _, s := range []*FullAccessScope{nil, {}, {Mode: "programs"}, {Mode: "unrestricted", Programs: exact.Programs}, {Mode: "programs", Programs: []ProgramRule{{Executable: "tool", Arguments: "any"}}}, {Mode: "programs", Programs: []ProgramRule{{Executable: "/usr/bin/tool"}}}} {
		if s.Validate() == nil || s.Allows(nil) || s.Covers(exact) {
			t.Fatalf("invalid scope accepted: %+v", s)
		}
	}
	for _, s := range []*FullAccessScope{exact, any, all} {
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if exact.Allows(nil) || exact.Allows(&ProgramInvocation{Executable: "/bin/sh"}) || exact.Allows(&ProgramInvocation{Executable: "/usr/bin/tool", Args: []string{"a", "b", "", "$(touch sentinel)"}}) {
		t.Fatal("program scope widened")
	}
	if !exact.Allows(&ProgramInvocation{Executable: "/usr/bin/tool", Args: exact.Programs[0].Args}) || !any.Allows(&ProgramInvocation{Executable: "/usr/bin/tool", Args: []string{"different"}}) || !all.Allows(nil) {
		t.Fatal("matching invocation denied")
	}
	if !all.Covers(any) || !any.Covers(exact) || !exact.Covers(exact) || exact.Covers(any) || any.Covers(all) {
		t.Fatal("incorrect coverage")
	}
	empty := &FullAccessScope{Mode: "programs", Programs: []ProgramRule{{Executable: "/usr/bin/tool", Arguments: "exact"}}}
	if !empty.Allows(&ProgramInvocation{Executable: "/usr/bin/tool"}) || empty.Allows(&ProgramInvocation{Executable: "/usr/bin/tool", Args: []string{""}}) {
		t.Fatal("empty argument confused with no arguments")
	}
}
func TestProgramInvocationValidationAcrossPlatforms(t *testing.T) {
	for _, p := range []string{"/usr/bin/tool", `C:\Program Files\Tool\tool.exe`, `\\server\share\tool.exe`} {
		if err := ValidateProgramInvocation(p, []string{"a b", ""}); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	for _, p := range []string{"tool", "./tool", "C:tool.exe", "/usr/../bin/tool", "/usr/bin/", "/usr/bin/tool\x00"} {
		if ValidateProgramInvocation(p, nil) == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if ValidateProgramInvocation("/bin/tool", []string{"a\x00b"}) == nil {
		t.Fatal("accepted NUL")
	}
}
func TestFullAccessProposalRequiresExplicitScope(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Hour)
	input := PermissionRequestInput{FullAccess: true, ResourceKind: "compute", ResourceID: "compute", ResourceDescription: "Host", Actions: []Action{Execute}, ExpiresAt: &until, Reason: "Run approved program"}
	if input.Normalize(now) == nil {
		t.Fatal("missing scope defaulted to unrestricted")
	}
	input.FullAccessScope = &FullAccessScope{Mode: "unrestricted"}
	if err := input.Normalize(now); err != nil {
		t.Fatal(err)
	}
	input.Additional = []PermissionRequestInput{{FullAccess: true, ResourceKind: "compute", ResourceID: "remote", ResourceDescription: "Remote", Actions: []Action{Execute}, Reason: "Remote task"}}
	if input.Normalize(now) == nil {
		t.Fatal("grouped missing scope accepted")
	}
	input.Additional = nil
	input.FullAccess = false
	if input.Normalize(now) == nil {
		t.Fatal("scope without Full Access accepted")
	}
}
