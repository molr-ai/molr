package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/agent"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
)

type liveSession struct {
	hidden                    bool
	client                    *runtimeclient.Client
	ctx                       context.Context
	events                    <-chan runtimeclient.Event
	connected                 bool
	loading, dirty, scheduled bool
	expired                   bool
}
type liveEvent runtimeclient.Event
type refreshLive struct{}
type snapshotMsg struct {
	chats            []conversation.Conversation
	status           agent.Status
	assistants       []assistantapi.Assistant
	target           core.ID
	conversation     conversation.Conversation
	messages         []conversation.Message
	older            string
	initial, history bool
	requests         []policy.PermissionRequest
	err              error
}
type sentMsg struct {
	id        core.ID
	text, key string
	message   conversation.Message
	err       error
}
type resolvedMsg struct {
	id      core.ID
	request policy.PermissionRequest
	err     error
}
type readMsg struct {
	id       core.ID
	sequence uint64
	err      error
}
type pendingSend struct {
	text, key string
	active    bool
}

func newConnectedModel(ctx context.Context, client *runtimeclient.Client, events <-chan runtimeclient.Event) model {
	m := newModel()
	m.assistants = nil
	m.input = newComposer()
	m.input.SetWidth(m.contentWidth())
	m.input.Focus()
	m.live = &liveSession{client: client, ctx: ctx, events: events}
	m.notice = "正在连接…"
	m.refresh(true)
	return m
}
func (m model) waitLive() tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-m.live.events:
			return liveEvent(event)
		case <-m.live.ctx.Done():
			return nil
		}
	}
}
func (m *model) scheduleLive() tea.Cmd {
	if m.live.expired {
		return nil
	}
	if m.live.loading {
		m.live.dirty = true
		return nil
	}
	if m.live.scheduled {
		return nil
	}
	m.live.scheduled = true
	return tea.Tick(750*time.Millisecond, func(time.Time) tea.Msg { return refreshLive{} })
}
func (m *model) loadLive(history bool) tea.Cmd {
	if m.live.loading {
		m.live.dirty = true
		return nil
	}
	m.live.loading = true
	client, ctx := m.live.client, m.live.ctx
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
			cursor := ""
			for {
				var page core.Page[assistantapi.Assistant]
				if out.err = client.Do(ctx, "GET", "/api/assistants?state=active&limit=200&cursor="+url.QueryEscape(cursor), nil, &page); out.err != nil {
					return out
				}
				out.assistants = append(out.assistants, page.Items...)
				if page.NextCursor == "" {
					break
				}
				if page.NextCursor == cursor {
					out.err = errors.New("助理分页游标没有前进")
					return out
				}
				cursor = page.NextCursor
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
			cursor := ""
			for {
				var page core.Page[conversation.Conversation]
				if out.err = client.Do(ctx, "GET", "/api/conversations?limit=200&cursor="+url.QueryEscape(cursor), nil, &page); out.err != nil {
					return out
				}
				out.chats = append(out.chats, page.Items...)
				for _, c := range page.Items {
					if c.ID == chat {
						out.conversation = c
					}
				}
				if page.NextCursor == "" {
					break
				}
				if page.NextCursor == cursor {
					out.err = errors.New("会话分页游标没有前进")
					return out
				}
				cursor = page.NextCursor
			}
			if out.err = client.Do(ctx, "GET", "/api/assistants/"+string(out.target)+"/status", nil, &out.status); out.err != nil {
				return out
			}
			if out.err = client.Do(ctx, "GET", "/api/permission-requests?assistant_id="+string(out.target), nil, &out.requests); out.err != nil {
				return out
			}
		}
		query := "order=desc&limit=50"
		if history {
			query += "&cursor=" + url.QueryEscape(before)
		} else if !initial {
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
				return out
			}
			out.messages = append(out.messages, page.Items...)
			out.older = page.NextCursor
			if initial || history || page.NextCursor == "" {
				break
			}
			if page.NextCursor == cursor {
				out.err = errors.New("消息分页游标没有前进")
				return out
			}
			cursor = page.NextCursor
		}
		return out
	}
}

func (m model) updateLive(event tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.live == nil {
		return m, nil, false
	}
	switch msg := event.(type) {
	case liveEvent:
		if msg.Connected && !m.live.connected && (strings.Contains(m.notice, "实时连接") || strings.HasPrefix(m.notice, "无法连接 Runtime")) {
			m.notice = ""
		}
		m.live.connected = msg.Connected
		if msg.Err != nil {
			m.liveError(msg.Err)
		}
		var refresh tea.Cmd
		if msg.Connected {
			refresh = m.scheduleLive()
		}
		return m, tea.Batch(m.waitLive(), refresh), true
	case refreshLive:
		m.live.scheduled = false
		return m, m.loadLive(false), true
	case snapshotMsg:
		m.live.loading = false
		if msg.err != nil {
			m.liveError(msg.err)
		} else {
			current := core.ID("")
			if len(m.assistants) > 0 {
				current = m.assistants[m.selected].id
				m.assistants[m.selected].composer = m.input
			}
			if !msg.history {
				previous := map[core.ID]assistant{}
				for _, a := range m.assistants {
					previous[a.id] = a
				}
				m.assistants = nil
				for _, remote := range msg.assistants {
					a, exists := previous[remote.ID]
					if !exists {
						a.composer = newComposer()
					}
					a.id, a.chat, a.name, a.role = remote.ID, remote.ConversationID, cleanText(remote.Name), cleanText(remote.Status)
					for _, c := range msg.chats {
						if c.ID == a.chat {
							a.unread = c.UnreadCount > 0
						}
					}
					m.assistants = append(m.assistants, a)
				}
				m.selected = 0
				for i, a := range m.assistants {
					if a.id == current {
						m.selected = i
					}
				}
				if len(m.assistants) > 0 && m.assistants[m.selected].id != current {
					m.input = m.assistants[m.selected].composer
					m.input.SetWidth(m.contentWidth())
					m.resizeComposer()
					m.focusPage()
					m.refresh(true)
				}
				m.pickerCursor = max(0, min(m.pickerCursor, len(m.matches())-1))
			}
			if i := m.assistantIndex(msg.target); i >= 0 {
				a := &m.assistants[i]
				follow := !m.live.hidden && m.selection == nil && m.page == chatPage && m.viewport.AtBottom()
				oldHeight := m.viewport.TotalLineCount()
				a.messages = mergeMessages(a.messages, msg.messages)
				for _, v := range msg.messages {
					a.sequence = max(a.sequence, v.Sequence)
				}
				a.loaded = true
				if msg.initial || msg.history {
					a.older = msg.older
				}
				if !msg.history {
					a.status = msg.status
					a.busy = msg.status.State == agent.Working
					a.unread = msg.conversation.UnreadCount > 0
					a.readSequence = max(a.readSequence, msg.conversation.LastReadSequence)
					a.requests = nil
					for _, q := range msg.requests {
						if q.State == "pending" {
							a.requests = append(a.requests, q)
						}
					}
					a.approval = ""
					if len(a.requests) > 0 {
						a.approval = "pending"
					}
					// Freeze the request being reviewed; a new event must never change Enter's target.
				}
				if i == m.selected {
					m.refresh(!msg.history && (msg.initial || follow))
					if msg.history {
						m.viewport.SetYOffset(m.viewport.YOffset + m.viewport.TotalLineCount() - oldHeight)
					}
				}
			}
			if m.notice == "正在连接…" || m.notice == "正在加载…" || m.notice == "已发送" {
				m.notice = ""
			}
			if len(m.assistants) == 0 {
				m.refresh(false)
			}
		}
		var next tea.Cmd
		if m.live.dirty {
			m.live.dirty = false
			next = m.scheduleLive()
		}
		if len(m.assistants) > 0 && !m.assistants[m.selected].loaded && msg.err == nil {
			next = m.scheduleLive()
		}
		return m, tea.Batch(next, m.markLiveRead()), true
	case sentMsg:
		i := m.assistantIndex(msg.id)
		if i < 0 {
			return m, nil, true
		}
		a := &m.assistants[i]
		a.pending.active = false
		if msg.err != nil {
			m.liveError(msg.err)
			if i == m.selected {
				m.notice = "发送未确认，按 Enter 重试同一条消息；" + m.notice
			}
			return m, nil, true
		}
		a.pending = pendingSend{}
		a.messages = mergeMessages(a.messages, []conversation.Message{msg.message})
		// Do not advance the fetch cursor: earlier concurrent messages may still be missing.
		if i == m.selected {
			if strings.TrimSpace(cleanText(m.input.Value())) == msg.text {
				m.input.Reset()
				m.resizeComposer()
			}
			m.refresh(m.selection == nil && m.viewport.AtBottom())
			m.notice = "已发送"
		} else if strings.TrimSpace(cleanText(a.composer.Value())) == msg.text {
			a.composer.Reset()
		}
		return m, m.scheduleLive(), true
	case resolvedMsg:
		if m.review == nil || m.review.ID != msg.id {
			return m, m.scheduleLive(), true
		}
		m.resolving = false
		if msg.err != nil {
			m.liveError(msg.err)
			return m, nil, true
		}
		m.notice = "授权申请已处理"
		m.review = nil
		var back tea.Cmd
		if m.page == approvalPage {
			back = m.goBack()
		}
		return m, tea.Batch(back, m.scheduleLive()), true
	case readMsg:
		if i := m.assistantIndex(msg.id); i >= 0 {
			m.assistants[i].reading = false
			if msg.err == nil {
				m.assistants[i].readSequence = max(m.assistants[i].readSequence, msg.sequence)
			}
		}
		if msg.err != nil {
			m.liveError(msg.err)
		}
		return m, nil, true
	}
	return m, nil, false
}
func (m model) assistantIndex(id core.ID) int {
	for i, a := range m.assistants {
		if a.id == id {
			return i
		}
	}
	return -1
}
func (m *model) liveError(err error) {
	var e *runtimeclient.Error
	if errors.As(err, &e) && e.Status == 401 {
		m.live.expired = true
		m.notice = "登录已过期，按 Ctrl+O 选择连接后重新登录"
		return
	}
	m.notice = cleanText(err.Error())
}
func mergeMessages(old []message, incoming []conversation.Message) []message {
	byID := map[core.ID]message{}
	for _, m := range old {
		byID[m.id] = m
	}
	for _, v := range incoming {
		text := v.Content.Text
		if c := v.Content.Card; c != nil {
			text = c.Title + "\n\n" + c.Summary
			if c.Kind == conversation.PermissionRequestCard {
				text += "\n\nCtrl+P 查看授权申请"
			}
		}
		if len(v.Content.AttachmentIDs) > 0 {
			text += fmt.Sprintf("\n\n[附件 %d 个，请在 WebUI 查看]", len(v.Content.AttachmentIDs))
		}
		byID[v.ID] = message{id: v.ID, sequence: v.Sequence, role: string(v.Sender), text: cleanText(text)}
	}
	out := make([]message, 0, len(byID))
	for _, m := range byID {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sequence < out[j].sequence })
	return out
}
func (m *model) markLiveRead() tea.Cmd {
	if m.live == nil || m.live.expired || m.live.hidden || len(m.assistants) == 0 || m.page != chatPage || m.selection != nil || !m.viewport.AtBottom() {
		return nil
	}
	a := &m.assistants[m.selected]
	if a.reading || a.sequence <= a.readSequence {
		return nil
	}
	a.reading = true
	id, chat, seq, client, ctx := a.id, a.chat, a.sequence, m.live.client, m.live.ctx
	return func() tea.Msg {
		err := client.Do(ctx, "POST", "/api/conversations/read", conversation.ReadRequest{ConversationID: chat, ThroughSequence: seq}, nil)
		return readMsg{id: id, sequence: seq, err: err}
	}
}
func (m model) sendLive(text string) (tea.Model, tea.Cmd) {
	if m.live.expired || m.live.hidden || len(m.assistants) == 0 {
		return m, nil
	}
	a := &m.assistants[m.selected]
	if !a.loaded || a.pending.active {
		return m, nil
	}
	if a.pending.text != text || a.pending.key == "" {
		a.pending = pendingSend{text: text, key: runtimeclient.NewKey()}
	}
	a.pending.active = true
	id, chat, key, client, ctx := a.id, a.chat, a.pending.key, m.live.client, m.live.ctx
	m.notice = "正在发送…"
	return m, func() tea.Msg {
		var message conversation.Message
		err := client.Do(ctx, "POST", "/api/conversations/messages", conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: key}, ConversationID: chat, Content: conversation.Content{Kind: conversation.Text, Text: text}}, &message)
		return sentMsg{id: id, text: text, key: key, message: message, err: err}
	}
}
