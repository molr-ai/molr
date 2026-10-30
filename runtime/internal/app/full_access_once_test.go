//go:build (linux && (amd64 || arm64)) || darwin

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/filetools"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func grantOnce(t *testing.T, f resourceAccessFixture, scope *policy.FullAccessScope, key string) policy.Grant {
	t.Helper()
	expires := time.Now().Add(time.Hour)
	g, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true, FullAccessScope: scope, Once: true}, ExpiresAt: &expires, Mutation: core.Mutation{IdempotencyKey: key}})
	need(t, err)
	return g
}
func usedGrant(t *testing.T, f resourceAccessFixture, id core.ID) policy.Grant {
	t.Helper()
	g, err := persistence.Load[policy.Grant](t.Context(), f.b.store, "security.grants", persistence.Key(f.owner.OwnerID, id))
	need(t, err)
	return g
}
func TestFullAccessOnceConcurrentAdmissionAndPinnedRechecks(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
	scope := &policy.FullAccessScope{Mode: "programs", Programs: []policy.ProgramRule{{Executable: "/usr/bin/printf", Arguments: "exact", Args: []string{"one-use"}}}}
	g := grantOnce(t, f, scope, "once")
	bad := tools.ShellRequest{ComputeID: f.compute.ID, FullAccess: true, Executable: "/usr/bin/printf", Args: []string{"wrong"}}
	if _, err := f.b.remote.shellExecute(t.Context(), f.execution, bad); err == nil {
		t.Fatal("bad scope allowed")
	}
	if usedGrant(t, f, g.ID).Used != nil {
		t.Fatal("validation consumed grant")
	}
	req := bad
	req.Args = []string{"one-use"}
	type result struct {
		e   core.Execution
		err error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := f.execution
			e.OperationID = core.ID(fmt.Sprintf("once-%d", i))
			_, err := f.b.remote.shellExecute(t.Context(), e, req)
			results <- result{e, err}
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	var winner core.Execution
	for result := range results {
		if result.err == nil {
			wins++
			winner = result.e
		} else if core.CodeOf(result.err) != core.Forbidden {
			t.Fatal(result.err)
		}
	}
	if wins != 1 {
		t.Fatalf("executions=%d", wins)
	}
	g = usedGrant(t, f, g.ID)
	if g.Used == nil || g.Used.OperationID != winner.OperationID || g.Used.ExecutionID != winner.Principal.ExecutionID || g.Version != 2 {
		t.Fatalf("usage: %+v", g)
	}
	q := policy.CheckRequest{Target: core.Object{Kind: "resource", ID: f.compute.ID}, ResourceID: f.compute.ID, Action: policy.Execute, FullAccess: true, OperationID: winner.OperationID, Program: &policy.ProgramInvocation{Executable: req.Executable, Args: req.Args}}
	for i := 0; i < 3; i++ {
		need(t, f.ad.policy.Require(t.Context(), winner.Principal, q))
	}
	if usedGrant(t, f, g.ID).Version != g.Version {
		t.Fatal("recheck consumed again")
	}
	if _, err := f.b.remote.shellExecute(t.Context(), winner, req); err == nil {
		t.Fatal("same operation launched twice")
	}
	if _, err := f.b.Services.Grants.UpdateGrant(t.Context(), f.owner, policy.UpdateGrantRequest{GrantID: g.ID, Actions: g.Actions, Scope: policy.Scope{FullAccess: true, FullAccessScope: scope}, ExpiresAt: g.ExpiresAt, Mutation: core.Mutation{IdempotencyKey: "restore-used", ExpectedVersion: g.Version}}); core.ReasonOf(err) != "grant_already_used" {
		t.Fatal("editing restored use", err)
	}
	replacement := grantOnce(t, f, scope, "replacement")
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: g.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-used", ExpectedVersion: g.Version}}))
	if err := f.ad.policy.Require(t.Context(), winner.Principal, q); err == nil {
		t.Fatal("running operation switched to replacement authorization")
	}
	if usedGrant(t, f, replacement.ID).Used != nil {
		t.Fatal("recheck consumed replacement")
	}
	fresh := f.execution
	fresh.OperationID = "fresh-operation"
	_, err := f.b.remote.shellExecute(t.Context(), fresh, req)
	need(t, err)
}
func TestFullAccessOnceFailureAndCachedToolResult(t *testing.T) {
	t.Run("failed-execution", func(t *testing.T) {
		f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
		g := grantOnce(t, f, &policy.FullAccessScope{Mode: "unrestricted"}, "once-fail")
		req := tools.ShellRequest{ComputeID: f.compute.ID, FullAccess: true}
		if _, err := f.b.remote.shellExecute(t.Context(), f.execution, req); err == nil {
			t.Fatal("empty command accepted")
		}
		if usedGrant(t, f, g.ID).Used != nil {
			t.Fatal("invalid command consumed authorization")
		}
		req.Script = "exit 7"
		e := f.execution
		e.OperationID = "execution-fails"
		raw, err := f.b.remote.shellExecute(t.Context(), e, req)
		need(t, err)
		var result struct{ Exit struct{ Code int } }
		need(t, json.Unmarshal(raw, &result))
		if result.Exit.Code != 7 {
			t.Fatal(string(raw))
		}
		if usedGrant(t, f, g.ID).Used == nil {
			t.Fatal("failure refunded use")
		}
	})
	t.Run("tool-replay", func(t *testing.T) {
		f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
		g := grantOnce(t, f, &policy.FullAccessScope{Mode: "unrestricted"}, "once-cache")
		executor := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: operation.NewService(f.b.store, operation.Options{Authorizer: f.ad.policy}), ShellExecute: f.b.shellTasks.Execute})
		raw, _ := json.Marshal(tools.ShellRequest{ComputeID: f.compute.ID, FullAccess: true, Script: "printf cached-once"})
		call := tools.Call{Execution: f.execution, Name: "shell_execute", Arguments: raw}
		first, err := executor.Execute(t.Context(), call)
		need(t, err)
		again, err := executor.Execute(t.Context(), call)
		need(t, err)
		if string(first.Content) != string(again.Content) {
			t.Fatal("retry did not reuse result")
		}
		if usedGrant(t, f, g.ID).Version != 2 {
			t.Fatal("replay consumed again")
		}
	})
}
func TestFullAccessOnceRunningTaskAndFiles(t *testing.T) {
	t.Run("running-rechecks", func(t *testing.T) {
		f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
		g := grantOnce(t, f, &policy.FullAccessScope{Mode: "unrestricted"}, "once-running")
		done := make(chan error, 1)
		go func() {
			_, err := f.b.remote.shellExecute(t.Context(), f.execution, tools.ShellRequest{ComputeID: f.compute.ID, FullAccess: true, Script: "sleep 0.7; printf still-running"})
			done <- err
		}()
		select {
		case err := <-done:
			need(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("task stalled")
		}
		if usedGrant(t, f, g.ID).Used == nil {
			t.Fatal("execution was not consumed")
		}
	})
	t.Run("file-operation", func(t *testing.T) {
		f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
		g := grantOnce(t, f, &policy.FullAccessScope{Mode: "unrestricted"}, "once-file")
		path := filepath.Join(t.TempDir(), "sample.txt")
		need(t, os.WriteFile(path, []byte("sample"), 0600))
		files := newResourceFileTools(resourceFileOptions{adapters: f.ad, router: f.b.remote})
		_, err := files.Read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: tools.FileTarget{FullAccess: true, Path: path}})
		need(t, err)
		if usedGrant(t, f, g.ID).Used == nil {
			t.Fatal("file operation bypassed one-use grant")
		}
		another := f.execution
		another.OperationID = "second-file"
		if _, err := files.Read(t.Context(), another, tools.FileReadRequest{FileTarget: tools.FileTarget{FullAccess: true, Path: path}}); core.CodeOf(err) != core.Forbidden {
			t.Fatal("second file allowed", err)
		}
	})
}

func TestFullAccessOnceMediaOutputUsesOriginalAdmission(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
	provider := mediaTestModel(t, f)
	g := grantOnce(t, f, &policy.FullAccessScope{Mode: "unrestricted"}, "once-media")
	filename := filepath.Join(t.TempDir(), "image.png")
	original := testPNG(t, color.White)
	need(t, os.WriteFile(filename, original, 0600))
	files := newResourceFileTools(resourceFileOptions{adapters: f.ad, router: f.b.remote})
	raw, err := files.Read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: tools.FileTarget{FullAccess: true, Path: filename}, ModelID: provider.Model.ID})
	need(t, err)
	var result filetools.MediaReadResult
	need(t, json.Unmarshal(raw, &result))
	if len(result.Media) != 1 || result.Media[0].SourceOperationID != f.execution.OperationID {
		t.Fatal("missing snapshot origin", string(raw))
	}
	next := f.execution
	next.OperationID = "hydrate-output"
	request := model.Request{Execution: next, Inputs: []model.Input{{Role: model.Tool, ReplyTo: "read", Media: result.Media}}}
	inputs, err := files.PrepareMedia(t.Context(), request, provider)
	need(t, err)
	if !bytes.Equal(inputs[0].Media[0].Data, original) {
		t.Fatal("one-use result could not be read")
	}
	used := usedGrant(t, f, g.ID)
	if used.Version != 2 {
		t.Fatal("hydration consumed again")
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: used.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-media-once", ExpectedVersion: used.Version}}))
	inputs, err = files.PrepareMedia(t.Context(), request, provider)
	need(t, err)
	if len(inputs[0].Media[0].Data) != 0 {
		t.Fatal("revoked media was still loaded")
	}
}
