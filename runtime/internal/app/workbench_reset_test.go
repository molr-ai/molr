package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/persistence"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
)

func TestWorkbenchResetRequiresWriteScopeAndFreshCheckpointWhileHidden(t *testing.T) {
	cfg := testConfig(t)
	cfg.Debug.Enabled = true
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	a := assistant.Assistant{ID: "reset-target", OwnerID: p.OwnerID, Name: "Reset target", State: assistant.Active, Version: 1}
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(p.OwnerID, a.ID), a)
	}))
	need(t, b.Services.Agent.(*agent.Engine).Lifecycle(ctx, p, a.ID, "initialize"))
	sequence := uint64(0)
	req := debugtrace.ResetRequest{Mutation: core.Mutation{IdempotencyKey: "reset"}, ExpectedSequence: &sequence}
	for _, tc := range []struct {
		name   string
		scopes []string
		want   int
	}{
		{"reader", []string{"debug.read"}, 403},
		{"writer", []string{"*"}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proof, err := b.Services.Auth.Reauthenticate(ctx, p, auth.ReauthenticateRequest{Password: core.NewSecret("correct-horse-battery-staple-2026!"), Purpose: "token.create"})
			need(t, err)
			token, err := b.Services.Auth.CreateToken(ctx, p, auth.CreateTokenRequest{Name: tc.name, Scopes: tc.scopes, ExpiresAt: time.Now().Add(time.Hour), Verification: proof})
			need(t, err)
			body, _ := json.Marshal(req)
			request := httptest.NewRequest("POST", "https://runtime.example/api/debug/assistants/"+string(a.ID)+"/context/reset", strings.NewReader(string(body)))
			request.RemoteAddr = "127.0.0.1:1234"
			request.Header.Set("X-Forwarded-Proto", "https")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token.Value.Reveal())
			response := httptest.NewRecorder()
			b.HTTP.Handler().ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("reset HTTP=%d want %d: %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
	checkpoint, err := b.Trace.Checkpoint(ctx, p, a.ID)
	need(t, err)
	var saved struct {
		Sequence uint64
		Context  struct{ Inputs []any }
	}
	need(t, json.Unmarshal(checkpoint.Data, &saved))
	if saved.Sequence != 1 || len(saved.Context.Inputs) != 0 {
		t.Fatalf("reset checkpoint=%s", checkpoint.Data)
	}

	runs, err := b.Trace.Runs(ctx, p, debugtrace.Query{AssistantID: a.ID})
	need(t, err)
	if len(runs.Items) != 1 || runs.Items[0].Trigger != "checkpoint" {
		t.Fatalf("reset missing checkpoint recording: %+v", runs)
	}
	run, err := b.Trace.Get(ctx, p, runs.Items[0].ID)
	need(t, err)
	saves := 0
	for _, step := range run.Steps {
		if step.Kind != "checkpoint.saved" {
			continue
		}
		saves++
		payload, err := b.Trace.Detail(ctx, p, run.ID, step.Sequence)
		need(t, err)
		if !strings.Contains(string(payload.Data), `"Cause":"context_reset"`) || !strings.Contains(string(payload.Data), `"Sequence":1`) {
			t.Fatalf("wrong reset snapshot: %s", payload.Data)
		}
	}
	if saves != 1 {
		t.Fatalf("reset save count: %d", saves)
	}
	req.Mutation.IdempotencyKey = "stale"
	if core.CodeOf(b.Trace.ResetContext(ctx, p, a.ID, req)) != core.Conflict {
		t.Fatal("stale checkpoint accepted")
	}
	req.ExpectedSequence = nil
	if core.CodeOf(b.Trace.ResetContext(ctx, p, a.ID, req)) != core.InvalidArgument {
		t.Fatal("missing expected sequence accepted")
	}
	need(t, showWorkbench(t, b, p, false))
	if core.CodeOf(b.Trace.ResetContext(ctx, p, a.ID, req)) != core.InvalidArgument {
		t.Fatal("visibility changed reset validation")
	}
}

func TestWorkbenchDeploymentDisablesContextReset(t *testing.T) {
	cfg := testConfig(t)
	cfg.Debug.Enabled = false
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	zero := uint64(0)
	err = b.Trace.ResetContext(t.Context(), p, "missing", debugtrace.ResetRequest{Mutation: core.Mutation{IdempotencyKey: "reset"}, ExpectedSequence: &zero})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("deployment gating: %v", err)
	}
}
