package tools

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestShellToolRejectsHostOptionsAndDoesNotReplayEffects(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "shell.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	auth := fixtureAuthorizer{revoked: &atomic.Bool{}}
	count := 0
	ex := NewExecutor(db, Options{Authorizer: auth, Operations: operation.NewService(db, operation.Options{Authorizer: auth}), ShellExecute: func(ctx context.Context, e core.Execution, r ShellRequest) (json.RawMessage, error) {
		count++
		return json.RawMessage(`{"Exit":{"Code":1},"Stdout":"partial effect"}`), nil
	}})
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "shell"}
	for _, raw := range []string{`{"compute_id":"compute","script":"id","root":"/"}`, `{"compute_id":"compute","script":"id","require_isolation":false}`, `{"compute_id":"compute","script":"id","environment":{"TOKEN":"secret"}}`} {
		if _, err := ex.Execute(t.Context(), Call{Execution: e, Name: "shell_execute", Arguments: json.RawMessage(raw)}); err == nil {
			t.Fatal("untrusted host option accepted")
		}
	}
	call := Call{Execution: e, Name: "shell_execute", Arguments: json.RawMessage(`{"compute_id":"compute","script":"echo partial; exit 1"}`)}
	first, err := ex.Execute(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ex.Execute(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || string(first.Content) != string(second.Content) {
		t.Fatal("failed shell replayed side effects")
	}
	call.Arguments = json.RawMessage(`{"compute_id":"compute","script":"different"}`)
	if _, err := ex.Execute(t.Context(), call); err == nil {
		t.Fatal("idempotency identity reused with different script")
	}
}

func TestPermissionListToolUsesCurrentIdentityAndRejectsSuppliedScopes(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "resources.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var revoked atomic.Bool
	auth := fixtureAuthorizer{revoked: &revoked}
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "resources"}
	calls := 0
	ex := NewExecutor(db, Options{Authorizer: auth, Operations: operation.NewService(db, operation.Options{Authorizer: auth}), PermissionList: func(ctx context.Context, p core.Principal) (json.RawMessage, error) {
		if p != e.Principal {
			t.Fatal("discovery used untrusted identity")
		}
		calls++
		return json.RawMessage(`{"Resources":[],"BuiltInStorage":[]}`), nil
	}})
	defs, err := ex.Definitions(t.Context(), e.Principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "permission_list" || defs[0].SideEffect {
		t.Fatal("unexpected resource discovery definition")
	}
	for _, raw := range []string{`null`, `[]`, `{"assistant_id":"other"}`, `{"path":"/"}`} {
		if _, err = ex.Execute(t.Context(), Call{Execution: e, Name: "permission_list", Arguments: json.RawMessage(raw)}); err == nil {
			t.Fatal("untrusted resource discovery scope accepted")
		}
	}
	_, err = ex.Execute(t.Context(), Call{Execution: e, Name: "permission_list", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	revoked.Store(true)
	if _, err = ex.Execute(t.Context(), Call{Execution: e, Name: "permission_list", Arguments: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("revoked identity replayed capabilities")
	}
	if calls != 1 {
		t.Fatalf("unexpected calls=%d", calls)
	}
}

func TestShellSessionArgumentsAndBoundedControl(t *testing.T) {
	for _, raw := range []string{`{"compute_id":"compute","tty":true}`, `{"compute_id":"compute","script":"sleep 90","background":true}`, `{"compute_id":"compute","script":"sleep 90","timeout_seconds":0}`} {
		r, err := parseShell(json.RawMessage(raw))
		if err != nil || (!r.TTY && r.Script == "") {
			t.Fatalf("%s: %+v %v", raw, r, err)
		}
	}
	for _, raw := range []string{`{"compute_id":"compute"}`, `{"compute_id":"compute","tty":true,"background":true}`, `{"compute_id":"compute","script":"true","timeout_seconds":-1}`} {
		if _, err := parseShell(json.RawMessage(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"action":"list"}`, `{"action":"read","task_id":"task","cursor":12}`, `{"action":"write","task_id":"task","data":"hello\n"}`, `{"action":"resize","task_id":"task","rows":24,"cols":80}`} {
		if _, err := ParseShellTask(json.RawMessage(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{"action":"write"}`, `{"action":"read","task_id":"task","cursor":-1}`, `{"action":"resize","task_id":"task","cols":80}`, `{"action":"interrupt","task_id":"task","data":"bad"}`, `{"action":"read","task_id":"task","owner_id":"other"}`, `{"action":"list"} {}`} {
		if _, err := ParseShellTask(json.RawMessage(raw)); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestShellForwardPortsValidation(t *testing.T) {
	for _, ports := range []string{`[0]`, `[65536]`, `[-1]`, `[80,80]`, `[1,2,3,4,5]`, `["80"]`, `[80.5]`} {
		if _, err := parseShell(json.RawMessage(`{"compute_id":"compute","script":"true","forward_ports":` + ports + `}`)); err == nil {
			t.Fatal("accepted", ports)
		}
	}
	for _, ports := range []string{`[]`, `[80]`, `[1,65535]`} {
		if _, err := parseShell(json.RawMessage(`{"compute_id":"compute","script":"true","forward_ports":` + ports + `}`)); err != nil {
			t.Fatal(err)
		}
	}
}
