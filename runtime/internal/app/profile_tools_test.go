package app

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

func TestProfileToolPersistenceConflictFileAccessAndContext(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { c.Sandbox.Enabled = false })
	p := &profileTools{assistants: f.b.Services.Assistants, workspace: &resourceFileTools{adapters: f.ad}}
	ex := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: f.b.Services.Operations.(operation.Service), ProfileUpdate: p.update})
	call := func(id string, args string) (tools.Result, error) {
		e := f.execution
		e.OperationID = core.ID(id)
		return ex.Execute(t.Context(), tools.Call{Execution: e, Name: "profile_update", Arguments: json.RawMessage(args)})
	}
	get := func() assistant.Assistant {
		a, err := f.b.Services.Assistants.Get(t.Context(), f.owner, f.execution.AssistantID)
		need(t, err)
		return a
	}
	first, err := call("rename", `{"name":"小满","avatar":{"type":"preset","id":"cat"},"expected_version":1}`)
	need(t, err)
	updated := get()
	if updated.Name != "小满" || updated.Avatar != "preset:cat" || updated.Version != 2 {
		t.Fatal(updated)
	}
	replay, err := call("rename", `{"name":"小满","avatar":{"type":"preset","id":"cat"},"expected_version":1}`)
	need(t, err)
	if string(replay.Content) != string(first.Content) || get().Version != 2 {
		t.Fatal("operation replay repeated effect")
	}
	events, err := realtime.NewStore(f.b.store).Replay(t.Context(), f.owner.OwnerID, realtime.Query{Topics: []string{"assistant.profile"}})
	need(t, err)
	if len(events.Items) != 1 || events.Items[0].Object.ID != updated.ID {
		t.Fatal("missing or duplicate profile event")
	}
	conflict, err := call("conflict", `{"name":"Outdated","expected_version":1}`)
	if core.CodeOf(err) != core.Conflict || conflict.Failure == nil || conflict.Failure.SideEffectsPossible || conflict.Failure.CurrentProfile == nil || conflict.Failure.CurrentProfile.Name != "小满" {
		t.Fatalf("conflict: %s %v", conflict.Content, err)
	}
	text, err := prompt.Structured("failure", string(conflict.Content))
	need(t, err)
	if !strings.Contains(text, `"Version":2`) || !strings.Contains(text, `"current_profile"`) {
		t.Fatal("conflict details lost in model prompt", text)
	}
	_, err = call("noop", `{"name":"小满","expected_version":2}`)
	need(t, err)
	if get().Version != 2 {
		t.Fatal("no-op changed version")
	}
	root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
	need(t, os.MkdirAll(root, 0700))
	need(t, os.WriteFile(filepath.Join(root, "avatar.png"), testPNG(t, color.White), 0600))
	need(t, os.WriteFile(filepath.Join(root, "bad.png"), []byte("invalid"), 0600))
	_, err = call("bad-image", `{"name":"Partial","avatar":{"type":"file","path":"bad.png"},"expected_version":2}`)
	if core.CodeOf(err) != core.InvalidArgument || get() != updated {
		t.Fatal("invalid avatar partially updated profile", err)
	}
	_, err = call("file", `{"avatar":{"type":"file","path":"avatar.png"},"expected_version":2}`)
	need(t, err)
	image := get()
	if image.Name != "小满" || !strings.HasPrefix(image.Avatar, "data:image/jpeg;base64,") {
		t.Fatal("file avatar failed")
	}
	doc, err := p.context(t.Context(), f.execution)
	need(t, err)
	text, err = prompt.Structured("profile", doc.Content)
	need(t, err)
	if !strings.Contains(text, `"Type":"image"`) || !strings.Contains(text, "Digest") || strings.Contains(text, "base64") || strings.Contains(text, "ModelID") {
		t.Fatal("unsafe or missing profile context", text)
	}
	_, err = call("clear", `{"avatar":{"type":"clear"},"expected_version":3}`)
	need(t, err)
	if get().Avatar != "" || get().Name != "小满" {
		t.Fatal("clear changed name")
	}
	_, err = call("storage-denied", `{"avatar":{"type":"file","resource_id":"`+string(f.storage.ID)+`","path":"/read/data"},"expected_version":4}`)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("ungranted storage readable", err)
	}
	need(t, os.WriteFile(filepath.Join(f.root, "read", "image"), testPNG(t, color.White), 0600))
	need(t, os.Symlink(filepath.Join(f.root, "read", "image"), filepath.Join(root, "link.png")))
	_, err = call("symlink", `{"avatar":{"type":"file","path":"link.png"},"expected_version":4}`)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("symlink readable", err)
	}
	f.grant(t, f.storage.ID, "read", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/read"}}, nil)
	_, err = call("storage-image", `{"avatar":{"type":"file","path":"/resources/`+string(f.storage.ID)+`/read/image"},"expected_version":4}`)
	need(t, err)
	_, err = call("outside-scope", `{"avatar":{"type":"file","path":"/resources/`+string(f.storage.ID)+`/secret/data"},"expected_version":5}`)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("read scope escaped", err)
	}
	e := f.execution
	e.Principal.ExecutionID = "expired"
	e.OperationID = "expired-profile"
	_, err = ex.Execute(t.Context(), tools.Call{Execution: e, Name: "profile_update", Arguments: json.RawMessage(`{"name":"Expired","expected_version":5}`)})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("expired execution accepted", err)
	}
	e = f.execution
	e.AssistantID = "other"
	e.OperationID = "foreign-profile"
	_, err = ex.Execute(t.Context(), tools.Call{Execution: e, Name: "profile_update", Arguments: json.RawMessage(`{"name":"Foreign","expected_version":5}`)})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("foreign assistant accepted", err)
	}
	e = f.execution
	e.Principal.OwnerID = "other-owner"
	e.OperationID = "foreign-owner"
	if _, err = ex.Execute(t.Context(), tools.Call{Execution: e, Name: "profile_update", Arguments: json.RawMessage(`{"name":"Foreign","expected_version":5}`)}); err == nil {
		t.Fatal("foreign owner accepted")
	}
}
