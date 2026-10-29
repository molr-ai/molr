package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
)

type commandResult struct {
	commandOnly   bool
	id            core.ID
	input, notice string
	err           error
}

var chatCommands = []string{"/retry-send", "/settings", "/new", "/status", "/permissions", "/tasks", "/plans", "/processes", "/files", "/search", "/messages", "/help", "/assistant", "/approval", "/connections", "/stop", "/停止", "/expedite", "/加急", "/compress", "/压缩", "/permission approve", "/permission reject", "/权限审批 同意", "/权限审批 拒绝", "/attach", "/attachments", "/drafts", "/channel", "/quit"}

func (m model) commandMatches() []string {
	value := m.input.Value()
	if m.commandOnly() {
		value = commandText(value)
	}
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "\n") || m.commandDismissed == m.input.Value() {
		return nil
	}
	var matches []string
	for _, command := range chatCommands {
		if strings.HasPrefix(command, value) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (m *model) commandKey(key tea.KeyMsg) bool {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return false
	}
	m.commandCursor = min(m.commandCursor, len(matches)-1)
	switch key.String() {
	case "up":
		m.commandCursor = (m.commandCursor + len(matches) - 1) % len(matches)
	case "down":
		m.commandCursor = (m.commandCursor + 1) % len(matches)
	case "esc":
		m.commandDismissed = m.input.Value()
	case "tab", "enter":
		if key.String() == "enter" && slices.Contains(matches, m.composerText()) {
			return false
		}
		value := matches[m.commandCursor]
		if m.commandOnly() {
			value = strings.TrimPrefix(value, "/")
		}
		m.input.SetValue(value)
		m.input.CursorEnd()
		m.commandDismissed = m.input.Value()
		m.resizeComposer()
	default:
		return false
	}
	return true
}

func (m model) runCommand(text string) (tea.Model, tea.Cmd, bool) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return m, nil, false
	}
	name := parts[0]
	if name == "/retry-send" {
		if len(parts) != 1 {
			m.notice = "此指令不接受参数"
			return m, nil, true
		}
		m.resetComposer()
		m.resizeComposer()
		next, cmd := m.sendPending()
		return next, cmd, true
	}
	switch name {
	case "/channel", "/settings", "/new", "/status", "/permissions", "/tasks", "/plans", "/processes", "/files", "/search", "/messages":
		m.resetComposer()
		m.resizeComposer()
		cmd := m.openManagement(name, strings.TrimSpace(strings.TrimPrefix(text, name)))
		return m, cmd, true
	}
	switch name {
	case "/attach":
		path := strings.TrimSpace(strings.TrimPrefix(text, name))
		m.resetComposer()
		m.resizeComposer()
		if path == "" {
			cmd := m.openFiles()
			return m, cmd, true
		}
		cmd := m.attachPath(path)
		return m, cmd, true
	case "/attachments", "/drafts":
		m.resetComposer()
		m.resizeComposer()
		cmd := m.openAttachments(name == "/drafts")
		return m, cmd, true
	case "/stop", "/停止", "/expedite", "/加急", "/compress", "/压缩", "/permission", "/权限审批":
	default:
		return m, nil, false
	}
	if m.live == nil || len(m.assistants) == 0 {
		m.notice = "此操作需要连接 Runtime"
		return m, nil, true
	}
	if m.live.expired {
		m.notice = "登录已过期，请重新登录"
		return m, nil, true
	}
	a := &m.assistants[m.selected]
	if a.commandBusy {
		m.notice = "正在处理上一条指令…"
		return m, nil, true
	}
	if name == "/停止" {
		name = "/stop"
	}
	if name == "/加急" {
		name = "/expedite"
	}
	if name == "/压缩" {
		name = "/compress"
	}
	if name != "/permission" && name != "/权限审批" && len(parts) != 1 {
		m.notice = "此指令不接受参数"
		return m, nil, true
	}
	var selected []policy.PermissionRequest
	decision, target := "", ""
	if name == "/permission" || name == "/权限审批" {
		if len(parts) < 2 || len(parts) > 3 {
			m.notice = "用法：/permission approve|reject [申请 ID|all]"
			return m, nil, true
		}
		decision = parts[1]
		if decision == "同意" {
			decision = "approve"
		}
		if decision == "拒绝" {
			decision = "reject"
		}
		if decision != "approve" && decision != "reject" {
			m.notice = "请选择 approve（同意）或 reject（拒绝）"
			return m, nil, true
		}
		if len(parts) == 3 {
			target = parts[2]
		}
		for _, q := range a.requests {
			if target == "all" || target == "全部" || target == string(q.ID) || target == "" && len(selected) == 0 {
				selected = append(selected, q)
			}
		}
		if len(selected) == 0 {
			m.notice = "没有匹配的待审批申请"
			return m, nil, true
		}
		if decision == "approve" {
			for _, q := range selected {
				if !approvalComplete(q) {
					m.notice = "申请需要补全资源或额度，请在 WebUI 审批表单处理"
					return m, nil, true
				}
			}
		}
	}
	through := uint64(0)
	if name == "/expedite" {
		if a.status.State != agent.Working {
			m.notice = "助理当前没有正在执行的任务"
			return m, nil, true
		}
		for _, msg := range a.messages {
			if msg.role == "user" && !msg.read && msg.id != a.status.TriggerMessageID && msg.sequence > a.status.InterruptedThroughSequence {
				through = max(through, msg.sequence)
			}
		}
		if through == 0 {
			m.notice = "没有可加急的未读消息，请先发送补充消息"
			return m, nil, true
		}
	}
	signature := fmt.Sprintf("%s:%d", text, through)
	for _, q := range selected {
		signature += fmt.Sprintf(":%s:%d", q.ID, q.Version)
	}
	if a.commandSignature != signature {
		a.commandSignature, a.commandKey = signature, runtimeclient.NewKey()
	}
	a.commandBusy = true
	id, chat, key, client, ctx := a.id, a.chat, a.commandKey, m.live.client, m.live.ctx
	commandOnly := m.commandOnly()
	m.notice = "正在处理指令…"
	return m, func() tea.Msg {
		out := commandResult{id: id, input: text, commandOnly: commandOnly}
		switch name {
		case "/stop":
			out.err = client.Do(ctx, "POST", "/api/assistants/"+string(id)+"/stop", struct{}{}, nil)
			out.notice = "已停止当前执行；后续新消息仍可唤醒助理"
		case "/compress":
			out.err = client.Do(ctx, "POST", "/api/assistants/"+string(id)+"/compress", agent.CompressionRequest{Mutation: core.Mutation{IdempotencyKey: key}}, nil)
			out.notice = "已请求压缩上下文"
		case "/expedite":
			var result agent.ExpediteStatus
			out.err = client.Do(ctx, "POST", "/api/assistants/"+string(id)+"/expedite", agent.ExpediteRequest{Mutation: core.Mutation{IdempotencyKey: key}, ConversationID: chat, ThroughSequence: through}, &result)
			out.notice = "已加急，当前操作结束后读取补充消息"
			if result.State == "not_running" || result.State == "not_needed" || result.State == "read" {
				out.notice = "消息已读取或当前无需加急"
			}
		default:
			var current []policy.PermissionRequest
			out.err = client.Do(ctx, "GET", "/api/permission-requests?assistant_id="+string(id), nil, &current)
			if out.err != nil {
				return out
			}
			current = slices.DeleteFunc(current, func(q policy.PermissionRequest) bool { return q.State != "pending" })
			if target == "" && (len(current) == 0 || current[0].ID != selected[0].ID) {
				out.err = fmt.Errorf("最新申请已变化，请刷新后重试")
				return out
			}
			for _, q := range selected {
				if !slices.ContainsFunc(current, func(c policy.PermissionRequest) bool {
					return c.ID == q.ID && c.Version == q.Version && c.AssistantID == id
				}) {
					out.err = fmt.Errorf("申请已变化，请刷新后重试")
					return out
				}
			}
			for i, q := range selected {
				r := policy.PermissionResolution{Decision: decision, RequestID: q.ID, Mutation: core.Mutation{IdempotencyKey: key + string(q.ID), ExpectedVersion: q.Version}}
				if decision == "approve" {
					proposal := approveInput(q.Input)
					proposal.Decision, proposal.RequestID, proposal.Mutation = r.Decision, r.RequestID, r.Mutation
					r = proposal
					for _, item := range q.Input.Additional {
						r.Additional = append(r.Additional, approveInput(item))
					}
				}
				if err := client.Do(ctx, "POST", "/api/permission-requests/resolve", r, nil); err != nil {
					out.err = fmt.Errorf("已处理 %d/%d 条，已停止：%w", i, len(selected), err)
					return out
				}
			}
			out.notice = fmt.Sprintf("已处理 %d 条授权申请", len(selected))
		}
		return out
	}, true
}

func approvalComplete(q policy.PermissionRequest) bool {
	for _, item := range append([]policy.PermissionRequestInput{q.Input}, q.Input.Additional...) {
		if item.ResourceID == "" || item.ResourceKind == "model" && item.ModelID == "" || !item.FullAccess && (item.ResourceKind == "compute" || item.ResourceKind == "network") && item.Constraints == nil {
			return false
		}
	}
	return true
}

func (m model) channelNotice() string {
	if m.live == nil || len(m.assistants) == 0 {
		return ""
	}
	a := m.assistants[m.selected]
	if !a.loaded || a.channel == "" {
		return "正在确认聊天入口，请稍候或按 Ctrl+R 重试"
	}
	if a.channel == "webui" {
		return ""
	}
	name := map[string]string{"weixin": "微信", "telegram": "Telegram", "slack": "Slack", "dingtalk": "钉钉", "feishu": "飞书", "qq": "QQ"}[a.channel]
	if name == "" {
		name = "外部渠道"
	}
	return "聊天入口：" + name + "。请在该渠道发消息；在 WebUI「切换聊天入口」解绑后恢复 TUI 输入。"
}
