package tools

import (
	"molr.ai/runtime/internal/core"
)

type AssistantRequest struct {
	Name            string       `json:"name,omitempty"`
	Instructions    string       `json:"instructions,omitempty"`
	ModelID         core.ID      `json:"model_id,omitempty"`
	GrantIDs        []core.ID    `json:"grant_ids,omitempty"`
	AssistantID     core.ID      `json:"assistant_id,omitempty"`
	ExpectedVersion core.Version `json:"expected_version,omitempty"`
}

func assistantArguments(call Call) (AssistantRequest, error) {
	var r AssistantRequest
	allowed := ""
	required := []string{}
	switch call.Name {
	case "assistant_list":
	case "assistant_hire":
		allowed = "name instructions model_id grant_ids"
		required = []string{"name", "instructions"}
	case "assistant_dismiss":
		allowed = "assistant_id expected_version"
		required = []string{"assistant_id", "expected_version"}
	default:
		return r, core.Invalid("tool", "unknown assistant tool")
	}
	err := decodeArguments(call.Arguments, &r, allowed, required...)
	return r, err
}
func (e *executor) assistantDefinitions() []Definition {
	if e.options.Assistant == nil {
		return nil
	}
	return []Definition{
		{Name: "assistant_list", Description: "List up to 200 contactable assistants in your account, with stable IDs and descriptions. The tool stays available when peer communication is disabled; the result is filtered by both peers' allowed counterparts and expiry. Your own temporary workers use hiring authorization; temporary assistants see only their employer. Use conversation_publish receiver to contact an assistant. Listing does not grant access to their files, memory or other conversations.", InputSchema: schema("", ""), RequiredActions: []string{"read"}},
		{Name: "assistant_hire", Description: "Hire an independent temporary assistant within the user's existing hiring authorization. Creation atomically delivers instructions. Omit model_id to use your model and grant_ids to use the preauthorized default selection. Permissions are fixed at creation. Workers cannot hire, request permissions or communicate with anyone except you. Active idle workers occupy slots; dismiss when finished. All workers share the hiring budget, including previous dismissed workers. End your turn while awaiting results; replies wake you without polling.", InputSchema: schema(`"name":{"type":"string","maxLength":256},"instructions":{"type":"string","maxLength":65536},"model_id":{"type":"string"},"grant_ids":{"type":"array","maxItems":64,"uniqueItems":true,"items":{"type":"string"}}`, `"name","instructions"`), RequiredActions: []string{"write"}, SideEffect: true},
		{Name: "assistant_dismiss", Description: "End employment of your temporary assistant, stopping execution and archiving its conversation while preserving delivered files. Use AssistantID and Version returned by hiring or assistant_list. This does not undo completed external writes.", InputSchema: schema(`"assistant_id":{"type":"string"},"expected_version":{"type":"integer","minimum":1}`, `"assistant_id","expected_version"`), RequiredActions: []string{"write"}, SideEffect: true},
	}
}
