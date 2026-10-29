package tui

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/storage/files"
	"net/url"
	"sync"
)

func (m *model) loadLive(history bool) tea.Cmd {
	if m.live.loading {
		m.live.dirty = true
		return nil
	}
	m.live.loading = true
	client, ctx := m.live.client, m.live.ctx
	known := map[core.ID]bool{}
	for id := range m.fileCache {
		known[id] = true
	}
	var id, chat core.ID
	var last uint64
	var before string
	initial := true
	if len(m.assistants) > 0 {
		a := m.assistants[m.selected]
		id, chat, last, before, initial = a.id, a.chat, a.sequence, a.older, !a.loaded
	}
	return func() tea.Msg {
		out := snapshotMsg{target: id, initial: initial, history: history}
		if !history {
			out.assistants, out.err = getAll[assistantapi.Assistant](ctx, client, "/api/assistants?state=active")
			if out.err != nil {
				return out
			}
			if id == "" && len(out.assistants) > 0 {
				out.target = out.assistants[0].ID
				chat = out.assistants[0].ConversationID
			}
			found := false
			for _, a := range out.assistants {
				if a.ID == out.target {
					found = true
				}
			}
			if !found {
				return out
			}
		}
		if chat == "" {
			return out
		}
		if !history {
			if out.err = out.loadDetails(ctx, client, chat); out.err != nil {
				return out
			}
		}
		out.err = out.loadMessages(ctx, client, chat, last, before, known)

		return out
	}
}

// Independent detail requests share a small concurrency bound. Results remain
// private until the complete snapshot is delivered to the model.
func (out *snapshotMsg) loadDetails(ctx context.Context, client *runtimeclient.Client, chat core.ID) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := []func() error{
		func() error {
			var err error
			out.chats, err = getAll[conversation.Conversation](ctx, client, "/api/conversations")
			for _, c := range out.chats {
				if c.ID == chat {
					out.conversation = c
				}
			}
			return err
		},
		func() error {
			return client.Do(ctx, "GET", "/api/assistants/"+string(out.target)+"/status", nil, &out.status)
		},
		func() error {
			return client.Do(ctx, "GET", "/api/assistants/"+string(out.target)+"/chat-channel", nil, &out.channel)
		},
		func() error {
			return client.Do(ctx, "GET", "/api/permission-requests?assistant_id="+string(out.target), nil, &out.requests)
		},
		func() error {
			var err error
			out.notices, err = getAll[notification.Notice](ctx, client, "/api/notifications?filter=active&assistant_id="+string(out.target))
			out.noticesOK = err == nil
			return nil // Notifications remain best effort.
		},
	}
	results := make([]chan error, len(jobs))
	pending := make(chan int, len(jobs))
	for i := range jobs {
		results[i] = make(chan error, 1)
		pending <- i
	}
	close(pending)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range pending {
				if err := ctx.Err(); err != nil {
					results[i] <- err
				} else {
					results[i] <- jobs[i]()
				}
			}
		}()
	}
	// Preserve request error priority without waiting for lower-priority reads.
	// Wait after cancellation so no worker can mutate a delivered snapshot.
	for _, result := range results {
		if err := <-result; err != nil {
			cancel()
			wg.Wait()
			return err
		}
	}
	wg.Wait()
	return nil
}

func (out *snapshotMsg) loadMessages(ctx context.Context, client *runtimeclient.Client, chat core.ID, last uint64, before string, known map[core.ID]bool) error {
	query := "order=desc&limit=50"
	if out.history {
		query += "&cursor=" + url.QueryEscape(before)
	} else if !out.initial {
		query = fmt.Sprintf("order=asc&limit=200&after_sequence=%d", last)
	}
	cursor := ""
	for {
		var page core.Page[conversation.Message]
		path := "/api/conversations/" + string(chat) + "/messages?" + query
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		if out.err = client.Do(ctx, "GET", path, nil, &page); out.err != nil {
			return out.err
		}
		out.messages = append(out.messages, page.Items...)
		for _, message := range page.Items {
			for _, id := range message.Content.AttachmentIDs {
				if known[id] {
					continue
				}
				known[id] = true
				var f files.File
				if err := client.Do(ctx, "GET", "/api/files/"+string(id), nil, &f); err == nil {
					if out.files == nil {
						out.files = map[core.ID]files.File{}
					}
					out.files[id] = f
				}
			}
		}
		out.older = page.NextCursor
		if out.initial || out.history || page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			out.err = errors.New("消息分页游标没有前进")
			return out.err
		}
		cursor = page.NextCursor
	}
	return nil
}
