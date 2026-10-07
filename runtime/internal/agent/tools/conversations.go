package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/storage/files"
)

type Attachment struct {
	SandboxID core.ID `json:"sandbox_id,omitempty"`
	FileID    core.ID `json:"file_id,omitempty"`
	Path      string  `json:"path,omitempty"`
	Name      string  `json:"name,omitempty"`
	MediaType string  `json:"media_type,omitempty"`
}
type ConversationRequest struct {
	SandboxID      core.ID        `json:"sandbox_id,omitempty"`
	ConversationID core.ID        `json:"conversation_id"`
	MessageID      core.ID        `json:"message_id,omitempty"`
	Cursor         string         `json:"cursor,omitempty"`
	Limit          int            `json:"limit,omitempty"`
	Text           string         `json:"text,omitempty"`
	Card           *cardArguments `json:"card,omitempty"`
	Attachments    []Attachment   `json:"attachments,omitempty"`
	FileID         core.ID        `json:"file_id,omitempty"`
	Path           string         `json:"path,omitempty"`
}
type ConversationAttachments interface {
	Get(context.Context, core.Principal, core.ID) (files.File, error)
	Upload(context.Context, core.Execution, Attachment, int) (core.ID, error)
	Import(context.Context, core.Execution, ConversationRequest) (json.RawMessage, error)
}

func decodeArguments(raw json.RawMessage, value any, allowed string, required ...string) error {
	if len(raw) > 3<<20 {
		return core.Invalid("arguments", "arguments too large")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return core.Invalid("arguments", "object required")
	}
	for key, v := range fields {
		if !strings.Contains(" "+allowed+" ", " "+key+" ") || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return core.Invalid(key, "unsupported argument")
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return core.Invalid(key, "required")
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return core.Invalid("arguments", "invalid arguments")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return core.Invalid("arguments", "trailing data")
	}
	return nil
}
func conversationArgs(call Call) (ConversationRequest, error) {
	var r ConversationRequest
	allowed := "conversation_id"
	required := []string{"conversation_id"}
	switch call.Name {
	case "conversation_read":
		allowed += " message_id cursor limit"
	case "conversation_publish":
		allowed += " text card attachments"
	case "conversation_attachment":
		allowed += " file_id path sandbox_id"
		required = append(required, "file_id", "path")
	}
	if err := decodeArguments(call.Arguments, &r, allowed, required...); err != nil {
		return r, err
	}
	if r.ConversationID.Validate() != nil {
		return r, core.Invalid("conversation_id", "required")
	}
	if r.MessageID != "" && (r.MessageID.Validate() != nil || r.Cursor != "") {
		return r, core.Invalid("message_id", "valid message ID without cursor required")
	}
	if r.Limit < 0 || r.Limit > 200 || len(r.Cursor) > 2048 {
		return r, core.Invalid("page", "invalid history page")
	}
	if call.Name == "conversation_publish" {
		if len(r.Attachments) > 20 || len(r.Text) > 65536 || (strings.TrimSpace(r.Text) == "" && r.Card == nil && len(r.Attachments) == 0) {
			return r, core.Invalid("content", "text, card or attachments required within limits")
		}
		if r.Card != nil && r.Text != "" {
			return r, core.Invalid("content", "card and text are mutually exclusive")
		}
		if r.Card != nil {
			if err := (conversation.Content{Kind: conversation.Card, Card: &conversation.CardContent{Kind: r.Card.Kind, TargetID: r.Card.TargetID, Title: r.Card.Title, Summary: r.Card.Summary}}).Validate(); err != nil {
				return r, err
			}
		}
		for _, a := range r.Attachments {
			if a.SandboxID != "" && (a.SandboxID != "local" || a.FileID != "") {
				return r, core.Invalid("sandbox_id", "sandbox ID requires a workspace path")
			}
			if (a.FileID == "") == (a.Path == "") || (a.FileID != "" && (a.FileID.Validate() != nil || a.Name != "" || a.MediaType != "")) {
				return r, core.Invalid("attachments", "provide file_id or workspace path")
			}
			if a.Path != "" {
				if err := workspaceFilePath(a.Path); err != nil {
					return r, err
				}
			}
		}
	}
	if call.Name == "conversation_attachment" {
		if r.SandboxID != "" && r.SandboxID != "local" {
			return r, core.Invalid("sandbox_id", "invalid sandbox ID")
		}
		if r.FileID.Validate() != nil {
			return r, core.Invalid("file_id", "required")
		}
		if err := workspaceFilePath(r.Path); err != nil {
			return r, err
		}
	}
	return r, nil
}
func workspaceFilePath(p string) error {
	raw, _ := json.Marshal(FileReadRequest{FileTarget: FileTarget{Path: p}})
	_, err := fileArguments(Call{Name: "file_read", Arguments: raw})
	return err
}
func validateConversationArguments(call Call) error { _, err := conversationArgs(call); return err }
func (e *executor) conversationDefinitions() []Definition {
	if e.options.Conversations == nil {
		return nil
	}
	ref := `"conversation_id":{"type":"string"}`
	defs := []Definition{
		{Name: "conversation_read", Description: "Read recent conversation messages (default 50, maximum 200), returned in chronological order within each page (oldest first, newest last). Use NextCursor as cursor to read older pages. Before replying, read all unread user messages: omit message_id to read recent history, then follow NextCursor until all messages not yet read in your context are covered. Process them in sequence order. If the previously read boundary is uncertain, continue to the end of history. message_id retrieves only one specific message and is not sufficient for checking all unread messages. Attachment metadata is returned with messages; use conversation_attachment to save an attachment to your workspace.", InputSchema: schema(ref+`,"message_id":{"type":"string"},"cursor":{"type":"string","maxLength":2048},"limit":{"type":"integer","minimum":1,"maximum":200}`, `"conversation_id"`), RequiredActions: []string{"read"}},
		{Name: "conversation_publish", Description: "The only way to communicate with the user: publish a user-visible message, card, and/or attachments. You MUST use this tool for answers, questions, progress updates, completion notices and error explanations. Direct assistant content and final replies are private; the user cannot see them and they are never published automatically. Check the result to confirm publication. Attachments accept either an accessible file_id or a file path in your workspace (relative or /work/...), with optional name and media_type. Paths always refer to the local workspace; download remote files using file_transfer first. Workspace files may be binary. Publication does not start another assistant turn.", InputSchema: schema(ref+`,"text":{"type":"string","maxLength":65536},"card":{"type":"object","properties":{"kind":{"enum":["task","resource","schedule"]},"target_id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"}},"required":["kind","target_id"],"additionalProperties":false},"attachments":{"type":"array","maxItems":20,"items":{"type":"object","properties":{"sandbox_id":{"enum":["local"]},"file_id":{"type":"string"},"path":{"type":"string"},"name":{"type":"string"},"media_type":{"type":"string"}},"additionalProperties":false}}`, `"conversation_id"`), RequiredActions: []string{"write"}, SideEffect: true},
	}
	if e.options.ConversationAttachments != nil {
		defs = append(defs, Definition{Name: "conversation_attachment", Description: "Save an attachment from this conversation into a new file in your own workspace, preserving binary bytes. Files are always saved locally. Use file_transfer afterwards to upload to a remote authorized directory. path must be a relative file path or /work/...; its parent must already exist. Never overwrites an existing file. Use file_read to inspect the saved file: text is returned directly, supported images/audio/video frames are loaded into model context, and unsupported formats return metadata.", InputSchema: schema(ref+`,"sandbox_id":{"enum":["local"]},"file_id":{"type":"string"},"path":{"type":"string","maxLength":4096}`, `"conversation_id","file_id","path"`), RequiredActions: []string{"read", "write"}, SideEffect: true})
	}
	return defs
}
func (e *executor) conversationDispatch(ctx context.Context, call Call) (json.RawMessage, error) {
	r, err := conversationArgs(call)
	if err != nil {
		return nil, err
	}
	if call.Name == "conversation_attachment" {
		return e.options.ConversationAttachments.Import(ctx, call.Execution, r)
	}
	if call.Name == "conversation_read" {
		page, err := e.options.Conversations.Messages(ctx, call.Execution.Principal, conversation.MessageQuery{ConversationID: r.ConversationID, MessageID: r.MessageID, Descending: true, Page: core.PageRequest{Limit: r.Limit, Cursor: r.Cursor}})
		if err != nil {
			return nil, err
		}
		// Keep the cursor from the descending query, but present each page oldest first.
		slices.Reverse(page.Items)
		attachments := map[core.ID]files.File{}
		if e.options.ConversationAttachments != nil {
			for _, m := range page.Items {
				for _, id := range m.Content.AttachmentIDs {
					f, err := e.options.ConversationAttachments.Get(ctx, call.Execution.Principal, id)
					if core.CodeOf(err) == core.NotFound {
						continue
					}
					if err != nil {
						return nil, err
					}
					attachments[id] = f
				}
			}
		}
		return json.Marshal(struct {
			core.Page[conversation.Message]
			Attachments map[core.ID]files.File
		}{page, attachments})
	}
	// Check the target before uploading anything.
	if _, err := e.options.Conversations.Get(ctx, call.Execution.Principal, r.ConversationID); err != nil {
		return nil, err
	}
	content := conversation.Content{Kind: conversation.Text, Text: r.Text}
	if r.Card != nil {
		content = conversation.Content{Kind: conversation.Card, Card: &conversation.CardContent{Kind: r.Card.Kind, TargetID: r.Card.TargetID, Title: r.Card.Title, Summary: r.Card.Summary}}
	}
	for i, a := range r.Attachments {
		if e.options.ConversationAttachments == nil {
			return nil, core.NewError(core.FailedPrecondition, "attachments unavailable")
		}
		id := a.FileID
		if id != "" {
			if _, err := e.options.ConversationAttachments.Get(ctx, call.Execution.Principal, id); err != nil {
				return nil, err
			}
		} else {
			id, err = e.options.ConversationAttachments.Upload(ctx, call.Execution, a, i)
			if err != nil {
				return nil, err
			}
		}
		content.AttachmentIDs = append(content.AttachmentIDs, id)
	}
	m, err := e.options.Conversations.Publish(ctx, call.Execution, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: string(call.Execution.OperationID)}, ConversationID: r.ConversationID, Content: content})
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}
