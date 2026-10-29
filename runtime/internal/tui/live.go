package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/agent"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/storage/files"
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
	notices          []notification.Notice
	noticesOK        bool
	files            map[core.ID]files.File
	channel          struct{ Channel string }
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
	clearVersion uint64
	attachments  []core.ID
	id           core.ID
	text, key    string
	message      conversation.Message
	err          error
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
	clearVersion uint64
	attachments  []core.ID
	text, key    string
	active       bool
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
			if out.err = client.Do(ctx, "GET", "/api/assistants/"+string(out.target)+"/chat-channel", nil, &out.channel); out.err != nil {
				return out
			}
			if out.err = client.Do(ctx, "GET", "/api/permission-requests?assistant_id="+string(out.target), nil, &out.requests); out.err != nil {
				return out
			}
			if notices, err := getAll[notification.Notice](ctx, client, "/api/notifications?filter=active&assistant_id="+string(out.target)); err == nil {
				out.notices = notices
				out.noticesOK = true
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
	case commandResult:
		if i := m.assistantIndex(msg.id); i >= 0 {
			a := &m.assistants[i]
			a.commandBusy = false
			if msg.err == nil {
				a.commandSignature = ""
				value := strings.TrimSpace(expandPasteBlocks(a.composer.Value(), a.pastes))
				if a.messageDraft != nil {
					value = commandText(a.composer.Value())
				}
				if i == m.selected {
					value = strings.TrimSpace(m.composerText())
				}
				if (a.messageDraft != nil) == msg.commandOnly && value == msg.input {
					if i == m.selected {
						m.resetComposer()
						m.resizeComposer()
					} else {
						a.composer.Reset()
						a.pastes = nil
					}
				}
			}
			if i == m.selected {
				if msg.err != nil {
					m.liveError(msg.err)
				} else {
					m.notice = msg.notice
				}
			}
		}
		return m, m.scheduleLive(), true
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
		if m.fileCache == nil {
			m.fileCache = map[core.ID]files.File{}
		}
		for id, f := range msg.files {
			m.fileCache[id] = f
		}
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
						a.id = remote.ID
						if m.drafts != nil {
							if err := m.drafts.restore(&a); err != nil {
								m.draftError = err.Error()
							}
						}
					}
					a.id, a.chat, a.name = remote.ID, remote.ConversationID, cleanText(remote.Name)
					for _, c := range msg.chats {
						if c.ID == a.chat {
							a.updateReadState(c)
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
				if m.preferredAssistant != "" {
					if i := m.assistantIndex(m.preferredAssistant); i >= 0 {
						m.selected = i
						m.preferredAssistant = ""
					}
				}
				if len(m.assistants) > 0 && m.assistants[m.selected].id != current {
					if m.page == filesPage || m.page == attachmentsPage {
						if m.files.cancel != nil {
							m.files.cancel()
						}
						m.page = chatPage
						m.back = nil
						m.files = fileDialog{}
					}
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
					if msg.noticesOK {
						a.notices = msg.notices
						slices.SortFunc(a.notices, func(a, b notification.Notice) int { return a.CreatedAt.Compare(b.CreatedAt) })
					}
					a.status = msg.status
					a.channel = msg.channel.Channel
					a.busy = msg.status.State == agent.Working
					a.updateReadState(msg.conversation)
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
		if m.live.dirty || len(m.assistants) > 0 && !m.assistants[m.selected].loaded && msg.err == nil {
			m.live.dirty = false
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
			if a.clearVersion != msg.clearVersion {
				a.pending = pendingSend{}
			}
			m.liveError(msg.err)
			if i == m.selected && a.clearVersion == msg.clearVersion {
				m.notice = "发送未确认，按 Enter 重试同一条消息；" + m.notice
			}
			return m, nil, true
		}
		a.pending = pendingSend{}
		a.attachments = slices.DeleteFunc(a.attachments, func(d draftAttachment) bool { return slices.Contains(msg.attachments, d.file.ID) })
		a.messages = mergeMessages(a.messages, []conversation.Message{msg.message})
		if a.messageDraft != nil && a.clearVersion == msg.clearVersion && strings.TrimSpace(expandPasteBlocks(a.messageDraft.Value(), a.messagePastes)) == msg.text {
			a.messageDraft.Reset()
			a.messagePastes = nil
		}
		// Do not advance the fetch cursor: earlier concurrent messages may still be missing.
		if i == m.selected {
			if !m.commandOnly() && a.clearVersion == msg.clearVersion && strings.TrimSpace(m.composerText()) == msg.text {
				m.resetComposer()
				m.resizeComposer()
			}
			m.refresh(m.selection == nil && m.viewport.AtBottom())
			m.notice = "已发送"
		} else if a.messageDraft == nil && a.clearVersion == msg.clearVersion && strings.TrimSpace(expandPasteBlocks(a.composer.Value(), a.pastes)) == msg.text {
			a.composer.Reset()
			a.pastes = nil
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
			a := &m.assistants[i]
			a.reading = false
			if msg.err == nil {
				a.readSequence = max(a.readSequence, msg.sequence)
				if msg.sequence >= a.unreadSequence {
					a.unreadCount = 0
				}
			}
		}
		if msg.err != nil {
			m.liveError(msg.err)
			return m, nil, true
		}
		return m, tea.Batch(m.markLiveRead(), m.scheduleLive()), true
	}
	return m, nil, false
}

// A snapshot started before a read acknowledgement must not restore its old count.
func (a *assistant) updateReadState(c conversation.Conversation) {
	if c.LastReadSequence < a.readSequence || c.LastSequence < a.unreadSequence {
		return
	}
	a.unreadCount, a.unreadSequence, a.readSequence = c.UnreadCount, c.LastSequence, c.LastReadSequence
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

		byID[v.ID] = message{attachments: slices.Clone(v.Content.AttachmentIDs), read: v.ReadAt != nil, id: v.ID, sequence: v.Sequence, role: string(v.Sender), text: cleanText(text)}
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
	if notice := m.channelNotice(); notice != "" {
		m.notice = notice
		return m, nil
	}
	if !a.loaded || a.pending.active {
		return m, nil
	}
	var ids []core.ID
	for _, d := range a.attachments {
		if d.busy || d.err != "" || d.file.ID == "" {
			m.notice = "附件尚未就绪，请等待上传或在 /drafts 重试"
			return m, nil
		}
		ids = append(ids, d.file.ID)
	}
	if a.pending.text != text || !slices.Equal(a.pending.attachments, ids) || a.pending.key == "" {
		if a.pending.key != "" {
			m.notice = "上次发送未确认，/retry-send 按原内容重试；Ctrl+C 可清除草稿后重新输入"
			return m, nil
		}
		a.pending = pendingSend{text: text, key: runtimeclient.NewKey(), attachments: slices.Clone(ids), clearVersion: a.clearVersion}
	}
	return m.sendPending()
}
func (m model) sendPending() (tea.Model, tea.Cmd) {
	if m.live == nil || m.live.expired || len(m.assistants) == 0 {
		return m, nil
	}
	a := &m.assistants[m.selected]
	if a.pending.key == "" || a.pending.active {
		m.notice = "没有待确认的发送"
		return m, nil
	}
	a.pending.active = true
	id, chat, key, client, ctx := a.id, a.chat, a.pending.key, m.live.client, m.live.ctx
	text, ids, clearVersion := a.pending.text, slices.Clone(a.pending.attachments), a.pending.clearVersion
	if err := m.persistDrafts(); err != nil {
		a.pending.active = false
		m.draftError = err.Error()
		return m, nil
	}
	m.notice = "正在确认发送结果…"
	return m, func() tea.Msg {
		var message conversation.Message
		err := client.Do(ctx, "POST", "/api/conversations/messages", conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: key}, ConversationID: chat, Content: conversation.Content{Kind: conversation.Text, Text: text, AttachmentIDs: ids}}, &message)
		return sentMsg{clearVersion: clearVersion, attachments: ids, id: id, text: text, key: key, message: message, err: err}
	}
}
