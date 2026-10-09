package debugtrace

import "encoding/json"

// ToolSummary is derived only from the bounded, sanitized capture. Counts are
// actual result items, never a requested limit or an estimated total.
type ToolSummary struct {
	Kind  string
	Text  string `json:",omitempty"`
	Count *int   `json:",omitempty"`
}

func toolSummary(kind, state string, encoded json.RawMessage) *ToolSummary {
	if kind != "tool.start" && kind != "tool.complete" {
		return nil
	}
	var data struct {
		Name      string
		Arguments map[string]json.RawMessage
		Result    struct{ Content json.RawMessage }
	}
	if json.Unmarshal(encoded, &data) != nil {
		return nil
	}
	text := func(fields map[string]json.RawMessage, keys ...string) *ToolSummary {
		for _, key := range keys {
			var value string
			if json.Unmarshal(fields[key], &value) == nil && value != "" {
				return &ToolSummary{Kind: "text", Text: shortPreview(value, 240)}
			}
		}
		return nil
	}
	count := func(kind string, raw json.RawMessage) *ToolSummary {
		var items []json.RawMessage
		// An explicitly captured null slice is an empty result; absent or
		// malformed fields must remain unknown rather than becoming zero.
		if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
			return nil
		}
		n := len(items)
		if kind == "attachments" && n == 0 {
			return nil
		}
		return &ToolSummary{Kind: kind, Count: &n}
	}
	if kind == "tool.complete" {
		if state != "succeeded" {
			return nil
		}
		var result map[string]json.RawMessage
		_ = json.Unmarshal(data.Result.Content, &result)
		switch data.Name {
		case "conversation_read":
			return count("messages", result["Items"])
		case "plan_list":
			return count("plans", result["Items"])
		case "conversation_publish":
			var content map[string]json.RawMessage
			if json.Unmarshal(result["Content"], &content) != nil {
				return nil
			}
			if summary := text(content, "Text"); summary != nil {
				return summary
			}
			var card map[string]json.RawMessage
			_ = json.Unmarshal(content["Card"], &card)
			if summary := text(card, "Title", "Summary"); summary != nil {
				return summary
			}
			return count("attachments", content["AttachmentIDs"])
		}
		return nil
	}
	args := data.Arguments
	switch data.Name {
	case "conversation_read", "plan_list", "permission_list":
		return nil
	case "conversation_publish":
		if summary := text(args, "text"); summary != nil {
			return summary
		}
		var card map[string]json.RawMessage
		_ = json.Unmarshal(args["card"], &card)
		if summary := text(card, "title", "summary"); summary != nil {
			return summary
		}
		return count("attachments", args["attachments"])
	case "file_read", "file_list", "file_write", "file_edit", "file_move", "file_delete", "file_transfer":
		return text(args, "path")
	case "shell_execute":
		return text(args, "script")
	case "shell_task", "plan_manage":
		return text(args, "title", "action")
	case "file_search":
		return text(args, "pattern", "path")
	case "permission_request":
		return text(args, "reason", "resource_description")
	case "conversation_attachment":
		return text(args, "path")
	}
	return text(args, "text", "query", "path", "script", "command", "pattern", "url", "title", "name", "action")
}
