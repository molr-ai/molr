package tools

import "encoding/json"

func permissionRequestDefinition() Definition {
	d := resourcePermissionRequestDefinition()
	d.Description = "Use permission_type=temporary_hiring to request or adjust your own temporary-hiring authorization: hiring={max_assistants,budget:{Currency,Micros},grant_ids}. Budget is the shared TOTAL allowance including previous spending; approval never resets it. Select existing grant IDs from permission_list. Empty grant_ids asks the user to choose. Use permission_type=assistant_communication with communication={all_assistants,assistant_ids} for your own assistant communication authorization. If specific assistant IDs are not visible, use all_assistants=false and an empty assistant_ids array, explain the desired collaborator in reason, and let the user choose during review. Do not invent or enumerate hidden IDs. For either delegation type, reason and expires_at are required; use an explicit future ISO time or null for permanent authorization. Permanent MUST be encoded as expires_at:null, never as a far-future date. Request only what the task needs. A delegation request creates one editable approval card, cannot contain resource fields or additional_requests, and never changes another assistant's authorization. Only the user's approval changes access. Temporary assistants cannot request permissions. permission_list includes current TaskDelegation authorizations; use existing access when sufficient. The following instructions apply to resource requests (permission_type omitted or resource): " + d.Description
	var root map[string]any
	if err := json.Unmarshal(d.InputSchema, &root); err != nil {
		panic(err)
	}
	properties := root["properties"].(map[string]any)
	properties["permission_type"] = map[string]any{"enum": []string{"resource", "temporary_hiring", "assistant_communication"}}
	properties["expires_at"] = map[string]any{"description": "Validity: delegation types accept null for permanent authorization; a date always means expiring authorization. Resource requests require a future date.", "anyOf": []any{map[string]any{"type": "string", "format": "date-time"}, map[string]any{"type": "null"}}}
	properties["hiring"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"max_assistants", "budget", "grant_ids"}, "properties": map[string]any{
		"max_assistants": map[string]any{"type": "integer", "minimum": 1, "maximum": 32},
		"budget":         map[string]any{"type": "object", "additionalProperties": false, "required": []string{"Currency", "Micros"}, "properties": map[string]any{"Currency": map[string]any{"type": "string"}, "Micros": map[string]any{"type": "integer", "minimum": 1}}},
		"grant_ids":      map[string]any{"type": "array", "maxItems": 64, "uniqueItems": true, "items": map[string]any{"type": "string"}},
	}}
	properties["communication"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"all_assistants", "assistant_ids"}, "properties": map[string]any{
		"all_assistants": map[string]any{"type": "boolean"}, "assistant_ids": map[string]any{"type": "array", "maxItems": 200, "uniqueItems": true, "items": map[string]any{"type": "string"}},
	}}
	resourceRequired := root["required"]
	root["required"] = []string{"reason", "expires_at"}
	root["oneOf"] = []any{
		map[string]any{"required": resourceRequired, "properties": map[string]any{"permission_type": map[string]any{"enum": []string{"resource"}}, "expires_at": map[string]any{"type": "string", "format": "date-time"}}},
		map[string]any{"required": []string{"permission_type", "hiring"}, "properties": map[string]any{"permission_type": map[string]any{"enum": []string{"temporary_hiring"}}}},
		map[string]any{"required": []string{"permission_type", "communication"}, "properties": map[string]any{"permission_type": map[string]any{"enum": []string{"assistant_communication"}}}},
	}
	d.InputSchema, _ = json.Marshal(root)
	return d
}
