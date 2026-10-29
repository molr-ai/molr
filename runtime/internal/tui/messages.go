package tui

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"slices"
	"sort"
)

func mergeMessages(old []message, incoming []conversation.Message) []message {
	merged, _ := mergeMessageChanges(old, incoming)
	return merged
}

// Read receipts affect commands but do not invalidate the rendered transcript.
func mergeMessageChanges(old []message, incoming []conversation.Message) ([]message, bool) {
	if len(incoming) == 0 {
		return old, false
	}
	index := make(map[core.ID]int, len(old))
	for i, m := range old {
		index[m.id] = i
	}
	out := old
	copied, contentChanged, added := false, false, false
	for _, v := range incoming {
		next := displayMessage(v)
		i, exists := index[v.ID]
		if exists {
			prior := out[i]
			same := prior.text == next.text && prior.role == next.role && prior.sequence == next.sequence && slices.Equal(prior.attachments, next.attachments)
			if same && prior.read == next.read {
				continue
			}
			if !copied {
				out = slices.Clone(old)
				copied = true
			}
			out[i] = next
			contentChanged = contentChanged || !same
			added = added || prior.sequence != next.sequence
		} else {
			if !copied {
				out = slices.Clone(old)
				copied = true
			}
			index[v.ID] = len(out)
			out = append(out, next)
			contentChanged, added = true, true
		}
	}
	if added {
		sort.Slice(out, func(i, j int) bool { return out[i].sequence < out[j].sequence })
	}
	return out, contentChanged
}
func displayMessage(v conversation.Message) message {
	text := v.Content.Text
	if c := v.Content.Card; c != nil {
		text = c.Title + "\n\n" + c.Summary
		if c.Kind == conversation.PermissionRequestCard {
			text += "\n\nCtrl+P 查看授权申请"
		}
	}

	return message{attachments: slices.Clone(v.Content.AttachmentIDs), read: v.ReadAt != nil, id: v.ID, sequence: v.Sequence, role: string(v.Sender), text: cleanText(text)}
}
