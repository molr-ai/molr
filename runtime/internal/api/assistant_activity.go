package api

import (
	"net/http"
	"strings"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
)

type assistantActivity struct {
	AssistantID    core.ID
	ConversationID core.ID
	Dreaming       bool
	Preview        *messagePreview
}
type messagePreview struct {
	Kind conversation.ContentKind
	Text string
}

// One bounded page for sidebar rendering. Existing services retain ownership
// checks; only the newest message per conversation is read and projected.
func (s *HTTPServer) assistantActivity(r *http.Request) (any, error) {
	page, err := queryPage(r)
	if err != nil {
		return nil, err
	}
	ctx, p := r.Context(), principal(r)
	assistants, err := s.services.Assistants.List(ctx, p, assistant.Query{State: assistant.Active, Page: page})
	if err != nil {
		return nil, err
	}
	ids := make([]core.ID, 0, len(assistants.Items))
	conversationIDs := make([]core.ID, 0, len(assistants.Items))
	for _, a := range assistants.Items {
		ids = append(ids, a.ID)
		conversationIDs = append(conversationIDs, a.ConversationID)
	}
	activities, err := s.services.Agent.Activities(ctx, p, ids)
	if err != nil {
		return nil, err
	}
	latest, err := s.services.Conversations.LatestMessages(ctx, p, conversationIDs)
	if err != nil {
		return nil, err
	}
	dreaming := make(map[core.ID]bool, len(activities))
	for _, a := range activities {
		dreaming[a.AssistantID] = a.Dreaming
	}
	result := core.Page[assistantActivity]{Items: make([]assistantActivity, 0, len(ids)), NextCursor: assistants.NextCursor}
	for _, a := range assistants.Items {
		row := assistantActivity{AssistantID: a.ID, ConversationID: a.ConversationID, Dreaming: dreaming[a.ID]}
		if message, ok := latest[a.ConversationID]; ok {
			content := message.Content
			text := content.Text
			if content.Kind == conversation.Card && content.Card != nil {
				text = content.Card.Title
				if text == "" {
					text = content.Card.Summary
				}
			}
			chars := []rune(strings.Join(strings.Fields(text), " "))
			if len(chars) > 80 {
				chars = append(chars[:80], '…')
			}
			row.Preview = &messagePreview{Kind: content.Kind, Text: string(chars)}
		}
		result.Items = append(result.Items, row)
	}
	return result, nil
}
