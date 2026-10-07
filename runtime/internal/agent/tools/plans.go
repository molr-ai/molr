package tools

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
)

type PlanRequest struct {
	Action          string                     `json:"action,omitempty"`
	PlanID          string                     `json:"plan_id,omitempty"`
	ExecutionID     core.ID                    `json:"execution_id,omitempty"`
	Cursor          string                     `json:"cursor,omitempty"`
	Limit           int                        `json:"limit,omitempty"`
	From            *time.Time                 `json:"from,omitempty"`
	To              *time.Time                 `json:"to,omitempty"`
	Mode            string                     `json:"mode,omitempty"`
	Title           string                     `json:"title,omitempty"`
	Instructions    string                     `json:"instructions,omitempty"`
	StartAt         *time.Time                 `json:"start_at,omitempty"`
	EndAt           *time.Time                 `json:"end_at,omitempty"`
	DueAt           *time.Time                 `json:"due_at,omitempty"`
	Timezone        string                     `json:"timezone,omitempty"`
	Recurrence      string                     `json:"recurrence,omitempty"`
	MissedPolicy    string                     `json:"missed_policy,omitempty"`
	MaxCatchUp      int                        `json:"max_catch_up,omitempty"`
	ExpectedVersion string                     `json:"expected_version,omitempty"`
	Dependencies    []string                   `json:"dependencies,omitempty"`
	Handoff         string                     `json:"handoff,omitempty"`
	Paused          bool                       `json:"paused,omitempty"`
	OccurrenceAt    *time.Time                 `json:"occurrence_at,omitempty"`
	Fields          map[string]json.RawMessage `json:"-"` // Presence distinguishes omitted values from explicit clearing.
}

const planFields = "title instructions start_at end_at due_at timezone recurrence missed_policy max_catch_up dependencies handoff paused"

func ParsePlanRequest(name string, raw json.RawMessage) (PlanRequest, error) {
	var r PlanRequest
	if len(raw) > 3<<20 {
		return r, core.Invalid("arguments", "arguments too large")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return r, core.Invalid("arguments", "object required")
	}
	var action string
	if b, ok := fields["action"]; ok {
		if json.Unmarshal(b, &action) != nil {
			return r, core.Invalid("action", "invalid action")
		}
	}
	var allowed string
	var required []string
	switch name {
	case "plan_list":
		allowed = "cursor limit from to"
	case "plan_read":
		allowed = "plan_id execution_id"
	case "plan_manage":
		required = []string{"action"}
		switch action {
		case "create":
			allowed = "action mode " + planFields
			required = append(required, "mode", "title")
		case "update":
			allowed = "action plan_id expected_version " + planFields
			required = append(required, "plan_id", "expected_version")
		case "delete":
			allowed = "action plan_id expected_version"
			required = append(required, "plan_id", "expected_version")
		case "execute":
			allowed = "action plan_id expected_version occurrence_at"
			required = append(required, "plan_id", "expected_version")
		default:
			return r, core.Invalid("action", "create, update, delete or execute required")
		}
	default:
		return r, core.Invalid("tool", "unknown plan tool")
	}
	// Only an update may explicitly clear optional dates. Other nulls remain invalid.
	normalized := map[string]json.RawMessage{}
	for key, value := range fields {
		if name == "plan_manage" && action == "update" && (key == "start_at" || key == "end_at" || key == "due_at") && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		normalized[key] = value
	}
	normalizedRaw, _ := json.Marshal(normalized)
	if err := decodeArguments(normalizedRaw, &r, allowed, required...); err != nil {
		return r, err
	}
	r.Fields = fields
	if r.Limit < 0 || r.Limit > 100 || len(r.Cursor) > 2048 {
		return r, core.Invalid("page", "invalid page")
	}
	if (r.From == nil) != (r.To == nil) || (r.From != nil && (!r.To.After(*r.From) || r.To.Sub(*r.From) > 125*24*time.Hour)) {
		return r, core.Invalid("range", "provide from and to within 125 days")
	}
	if name == "plan_read" && (r.PlanID == "") == (r.ExecutionID == "") {
		return r, core.Invalid("plan_id", "provide plan_id or current execution_id")
	}
	if r.PlanID != "" {
		if _, err := PlanReference(r.PlanID); err != nil {
			return r, err
		}
	}
	if name == "plan_manage" && action != "create" {
		if r.PlanID == "" {
			return r, core.Invalid("plan_id", "required")
		}
		if r.ExpectedVersion == "" || len(r.ExpectedVersion) > 128 {
			return r, core.Invalid("expected_version", "use Version returned by plan_read")
		}
	}
	if action == "create" && r.Mode != "once" && r.Mode != "scheduled" {
		return r, core.Invalid("mode", "once or scheduled required")
	}
	if _, present := fields["title"]; present {
		if strings.TrimSpace(r.Title) == "" || len(r.Title) > 4096 {
			return r, core.Invalid("title", "bounded nonempty title required")
		}
	}
	if len(r.Instructions) > 65536 || len(r.Recurrence) > 1024 {
		return r, core.Invalid("content", "plan content too long")
	}
	if action == "update" {
		changed := false
		for _, key := range strings.Fields(planFields) {
			if _, ok := fields[key]; ok {
				changed = true
			}
		}
		if !changed {
			return r, core.Invalid("update", "provide at least one field to change")
		}
	}
	if _, ok := fields["handoff"]; ok && r.Handoff != "notify" && r.Handoff != "automatic" {
		return r, core.Invalid("handoff", "notify or automatic required")
	}
	if len(r.Dependencies) > 32 {
		return r, core.Invalid("dependencies", "maximum 32 dependencies")
	}
	for _, id := range r.Dependencies {
		if _, err := PlanReference(id); err != nil {
			return r, err
		}
	}
	return r, nil
}
func PlanReference(id string) (core.Object, error) {
	kind, value, ok := strings.Cut(id, ":")
	if !ok || (kind != "todo" && kind != "schedule") || core.ID(value).Validate() != nil {
		return core.Object{}, core.Invalid("plan_id", "use an opaque plan_id returned by a plan tool")
	}
	return core.Object{Kind: kind, ID: core.ID(value)}, nil
}
func validatePlanArguments(call Call) error {
	_, err := ParsePlanRequest(call.Name, call.Arguments)
	return err
}
func (e *executor) planDefinitions() []Definition {
	if e.options.Plan == nil {
		return nil
	}
	ref := `"plan_id":{"type":"string","description":"Opaque PlanID returned by plan_list, plan_read or plan_manage"}`
	fields := `"title":{"type":"string","minLength":1,"maxLength":4096},"instructions":{"type":"string","maxLength":65536},"start_at":{"type":["string","null"],"format":"date-time"},"end_at":{"type":["string","null"],"format":"date-time"},"due_at":{"type":["string","null"],"format":"date-time"},"timezone":{"type":"string"},"recurrence":{"type":"string","maxLength":1024},"missed_policy":{"enum":["skip","coalesce","catch_up"]},"max_catch_up":{"type":"integer","minimum":0,"maximum":100},"dependencies":{"type":"array","maxItems":32,"items":{"type":"string"}},"handoff":{"enum":["notify","automatic"]},"paused":{"type":"boolean"}`
	own := " Only plans assigned to you are accessible. Times use RFC3339 offsets and scheduled plans require an IANA timezone."
	return []Definition{
		{Name: "plan_list", Description: "List your plans with bounded pagination. Use NextCursor to continue; optional from/to expands scheduled occurrences within at most 125 days." + own, InputSchema: schema(`"cursor":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":100},"from":{"type":"string","format":"date-time"},"to":{"type":"string","format":"date-time"}`, ""), RequiredActions: []string{"read"}},
		{Name: "plan_read", Description: "Read a plan, its opaque Version, instructions, dependencies and latest 20 execution summaries. Provide plan_id, or execution_id from the current Runtime event to resolve its plan and immutable CurrentExecution instruction snapshot." + own, InputSchema: schema(ref+`,"execution_id":{"type":"string"}`, ""), RequiredActions: []string{"read"}},
		{Name: "plan_manage", Description: "Create, update, delete or execute a plan. create requires mode and title: once creates an unscheduled plan without starting it (optional start_at/end_at time block); scheduled requires start_at/timezone and triggers automatically, with optional RRULE and missed_policy (default skip). update, delete and execute require plan_id and expected_version from Version. update changes only supplied fields, atomically including content, dates, dependencies, handoff and paused; omitted fields stay unchanged. Clear optional dates with null, instructions/recurrence with an empty string, dependencies with []; mode and assignee cannot change. Dependencies must be accessible one-time plans. handoff=notify leaves ready work pending, automatic enables dispatch after prerequisites. paused stops future starts, not an active execution. delete rejects plans with execution history or dependents; use update with paused=true instead. execute queues one run, requires occurrence_at for repeating schedules, and does not declare completion. Do not pass unrelated fields for an action." + own, InputSchema: schema(`"action":{"enum":["create","update","delete","execute"]},`+ref+`,"expected_version":{"type":"string","maxLength":128},"mode":{"enum":["once","scheduled"]},`+fields+`,"occurrence_at":{"type":"string","format":"date-time"}`, `"action"`), RequiredActions: []string{"write"}, SideEffect: true},
	}
}
