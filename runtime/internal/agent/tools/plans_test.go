package tools

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"testing"
)

func TestPlanRequestValidationAndOpaqueReferences(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"plan_manage", `{"action":"create","mode":"todo","title":"Plan"}`, true},
		{"plan_manage", `{"action":"create","mode":"todo","title":"Plan","assistant_id":"foreign"}`, false},
		{"plan_manage", `{"action":"update","plan_id":"todo:one","paused":true,"expected_version":"revision"}`, true},
		{"plan_manage", `{"action":"update","plan_id":"todo:one","title":"Plan","expected_version":0}`, false},
		{"plan_manage", `{"action":"configure","plan_id":"todo:one","paused":true,"expected_version":"revision"}`, false},
		{"plan_manage", `{"action":"delete","plan_id":"todo:one","title":"irrelevant","expected_version":"revision"}`, false},
		{"plan_manage", `{"action":"update","plan_id":"todo:one","expected_version":"revision"}`, false},
		{"plan_manage", `{"action":"update","plan_id":"todo:one","expected_version":"revision","start_at":null,"end_at":null}`, true},
		{"plan_manage", `{"action":"update","plan_id":"todo:one","expected_version":"revision","title":null}`, false},
		{"plan_manage", `{"action":"update","plan_id":"todo:one","expected_version":"revision","dependencies":null}`, false},
		{"plan_manage", `{"action":"update","plan_id":"todo:one","expected_version":"revision","dependencies":["invalid"]}`, false},
		{"plan_create", `{"mode":"todo","title":"Plan"}`, false},
		{"plan_read", `{"execution_id":"execution"}`, true},
		{"plan_read", `{"execution_id":"execution","plan_id":"todo:one"}`, false},
		{"plan_list", `{"from":"2026-01-01T00:00:00Z"}`, false},
	} {
		_, err := ParsePlanRequest(tc.name, json.RawMessage(tc.raw))
		if (err == nil) != tc.valid {
			t.Errorf("%s %s: %v", tc.name, tc.raw, err)
		}
	}
	ref, err := PlanReference("schedule:one")
	if err != nil || ref != (core.Object{Kind: "schedule", ID: "one"}) {
		t.Fatal(ref, err)
	}
	for _, id := range []string{"", "task:one", "todo:", "../one"} {
		if _, err := PlanReference(id); err == nil {
			t.Fatalf("accepted %s", id)
		}
	}
}
