package tui

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/agent"
	modelapi "molr.ai/runtime/internal/agent/model"
	assistantapi "molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/search"
)

func (m *model) openManagement(name, arg string) tea.Cmd {
	if m.live == nil {
		m.notice = "此操作需要连接 Runtime"
		return nil
	}
	id := m.assistantID()
	if id == "" && name != "/new" && name != "/help" {
		return m.assistantForm(true)
	}
	switch name {
	case "/new":
		return m.assistantForm(true)
	case "/settings":
		return m.assistantForm(false)
	case "/status":
		return m.statusScreen(id)
	case "/approval":
		return m.approvalsScreen(id)
	case "/permissions":
		return m.permissionsScreen(id)
	case "/tasks":
		return m.tasksScreen(id)
	case "/plans":
		return m.plansScreen(id)
	case "/processes":
		return m.processesScreen(id)
	case "/files":
		return m.workspaceScreen(id, ".")
	case "/channel":
		return m.channelScreen(id)
	case "/search":
		return m.searchForm(id, arg)
	case "/messages":
		return m.messagesScreen(id)
	}
	return nil
}
func (m *model) assistantForm(create bool) tea.Cmd {
	id := m.assistantID()
	title := "助理设置"
	if create {
		title = "创建助理"
	}
	return m.loadScreen(title, func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		a := assistantapi.Assistant{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}}
		if !create {
			if err := c.Do(ctx, "GET", "/api/assistants/"+string(id), nil, &a); err != nil {
				return manageScreen{}, err
			}
		}
		var models []modelapi.Model
		if err := c.Do(ctx, "GET", "/api/models", nil, &models); err != nil {
			return manageScreen{}, err
		}
		var grants []policy.Grant
		if !create {
			var err error
			grants, err = getAll[policy.Grant](ctx, c, "/api/grants?assistant_id="+string(id))
			if err != nil {
				return manageScreen{}, err
			}
		}
		mf := field("model", "模型", string(a.ModelID))
		mf.choices = []string{}
		mf.labels = []string{}
		for _, v := range models {
			allowed := create
			for _, g := range grants {
				if g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(time.Now())) && g.ResourceID == v.ResourceID && g.Scope.ModelID == v.ID && slices.Contains(g.Actions, policy.Execute) {
					allowed = true
				}
			}
			if allowed && v.Pricing != nil {
				mf.choices = append(mf.choices, string(v.ID))
				mf.labels = append(mf.labels, v.Name)
			}
		}
		if len(mf.choices) == 0 {
			if create {
				return manageScreen{title: title, body: "没有价格已配置的可用模型，请管理员检查模型连接与账号分配。Ctrl+R 重新检查。"}, nil
			}
			return manageScreen{title: title, body: "没有可选择的已授权模型。请先在权限页授予具体模型；没有资源时需要管理员分配。", rows: []manageRow{{"打开权限", func(m *model) tea.Cmd { return m.permissionsScreen(id) }}}}, nil
		}
		if !slices.Contains(mf.choices, mf.input.Value()) {
			mf.input.SetValue(mf.choices[0])
		}
		op := mutation(a.Version)
		var created assistantapi.Assistant
		s := manageScreen{title: title, body: "←→ 选择模型和周期。预算为 0 时仅允许零费用操作。\n更换模型只使用已有授权；其他权限在权限页管理。", fields: []formField{field("name", "名称", a.Name), mf, field("budget", "预算金额", moneyNumber(a.Budget.Limit)), field("currency", "币种", a.Budget.Limit.Currency, "USD", "CNY"), field("period", "预算周期", a.Budget.Period, "daily", "weekly"), field("context", "压缩阈值 tokens（0 自动）", strconv.Itoa(a.ContextCompressionTokens))}, submitLabel: "保存", returnTo: "/status"}
		s.submit = func(v map[string]string) (func(context.Context, *runtimeclient.Client) error, error) {
			name := strings.TrimSpace(v["name"])
			if name == "" {
				return nil, fmt.Errorf("请填写名称")
			}
			amount, err := parseMoney(v["budget"])
			if err != nil {
				return nil, err
			}
			tokens, err := strconv.Atoi(v["context"])
			if err != nil || tokens < 0 {
				return nil, fmt.Errorf("压缩阈值请输入非负整数")
			}
			budget := core.AssistantBudget{Limit: core.Money{Currency: v["currency"], Micros: amount}, Period: v["period"]}
			return func(ctx context.Context, c *runtimeclient.Client) error {
				if create {
					return c.Do(ctx, "POST", "/api/assistants", assistantapi.HireRequest{Mutation: op, Name: name, ModelID: core.ID(v["model"]), Budget: budget, ContextCompressionTokens: tokens}, &created)
				}
				// Read before mutation to detect changes without overwriting another client's fields.
				var current assistantapi.Assistant
				if err := c.Do(ctx, "GET", "/api/assistants/"+string(id), nil, &current); err != nil {
					return err
				}
				if current.Version != a.Version {
					return fmt.Errorf("助理设置已变化。草稿保留，请返回并重新打开核对")
				}
				if core.ID(v["model"]) != a.ModelID {
					grants, err := getAll[policy.Grant](ctx, c, "/api/grants?assistant_id="+string(id))
					if err != nil {
						return err
					}
					valid := false
					for _, g := range grants {
						if g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(time.Now())) && g.Scope.ModelID == core.ID(v["model"]) && slices.Contains(g.Actions, policy.Execute) {
							valid = true
						}
					}
					if !valid {
						return fmt.Errorf("所选模型授权已失效，请先到权限页处理")
					}
				}
				return c.Do(ctx, "POST", "/api/assistants/update", assistantapi.UpdateRequest{Mutation: op, ID: id, Name: name, ModelID: core.ID(v["model"]), Budget: budget, ContextCompressionTokens: &tokens}, nil)
			}, nil
		}
		if create {
			s.afterSuccess = func(m *model) tea.Cmd {
				m.preferredAssistant = created.ID
				m.manage.stack = nil
				m.back = nil
				m.page = chatPage
				m.notice = "助理已创建，正在加载…"
				return tea.Batch(m.input.Focus(), m.scheduleLive())
			}
		}
		return s, nil
	})
}
func moneyNumber(m core.Money) string {
	return fmt.Sprintf("%d.%06d", m.Micros/1000000, m.Micros%1000000)
}
func parseMoney(value string) (int64, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) > 2 || len(parts) == 0 {
		return 0, fmt.Errorf("预算最多六位小数")
	}
	whole, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || whole < 0 || whole > 1000000000 {
		return 0, fmt.Errorf("预算请输入非负金额")
	}
	frac := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) > 6 {
			return 0, fmt.Errorf("预算最多六位小数")
		}
		frac, e = strconv.ParseInt(parts[1]+strings.Repeat("0", 6-len(parts[1])), 10, 64)
		if e != nil || frac < 0 {
			return 0, fmt.Errorf("预算金额无效")
		}
	}
	return whole*1000000 + frac, nil
}
func reasonText(reason string) string {
	names := map[string]string{"permission_pending": "等待授权", "assistant_budget_missing": "请设置助理预算", "model_not_configured": "尚未选择模型", "model_unavailable": "模型当前不可用", "model_not_authorized": "模型尚未授权", "model_pricing_missing": "模型价格未配置，请管理员检查", "model_authentication_failed": "模型认证失败，请管理员检查凭据", "model_quota_exceeded": "模型额度不足", "model_retry_exhausted": "模型重试次数已用尽", "model_rate_limited": "模型服务限流", "model_service_unavailable": "模型服务暂不可用", "local_environment_unavailable": "执行环境不可用，请管理员检查沙盒", "budget_exceeded": "预算不足", "resource_quota_exceeded": "资源额度不足", "model_context_exceeded": "模型上下文超限，可请求压缩"}
	if text, ok := names[reason]; ok {
		return text
	}
	if reason == "" {
		return "无"
	}
	return cleanText(reason)
}
func stateText(state string) string {
	v := map[string]string{"idle": "空闲", "working": "处理中", "dreaming": "整理记忆", "waiting": "等待中", "paused": "已暂停", "stopped": "已停止", "running": "运行中", "pending": "待处理", "completed": "已完成", "failed": "失败", "canceled": "已取消"}
	if s, ok := v[state]; ok {
		return s
	}
	return state
}
func (m *model) statusScreen(id core.ID) tea.Cmd {
	var pending pendingSend
	if i := m.assistantIndex(id); i >= 0 {
		pending = m.assistants[i].pending
	}
	return m.loadScreen("执行状态", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var st agent.Status
		if err := c.Do(ctx, "GET", "/api/assistants/"+string(id)+"/status", nil, &st); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "执行状态", body: fmt.Sprintf("状态：%s\n更新：%s\n原因：%s", stateText(string(st.State)), st.UpdatedAt.Local().Format(time.DateTime), reasonText(st.WaitingReason))}
		if st.ModelRetry != nil {
			s.body += fmt.Sprintf("\n模型重试 %d/%d，下次 %s", st.ModelRetry.Attempt, st.ModelRetry.MaxAttempts, st.ModelRetry.NextAt.Local().Format(time.DateTime))
		}
		s.rows = []manageRow{{"查看待审批", func(m *model) tea.Cmd { return m.approvalsScreen(id) }}, {"助理设置与预算", func(m *model) tea.Cmd { return m.assistantForm(false) }}, {"查看资源授权", func(m *model) tea.Cmd { return m.permissionsScreen(id) }}, {"查看业务任务", func(m *model) tea.Cmd { return m.tasksScreen(id) }}, {"查看后台进程", func(m *model) tea.Cmd { return m.processesScreen(id) }}}
		if pending.key != "" {
			s.rows = append([]manageRow{{"查看上次未确认发送", func(m *model) tea.Cmd { return m.pendingScreen(id, pending) }}}, s.rows...)
		}
		if st.ActiveTaskID != "" {
			s.rows = append([]manageRow{{"打开当前任务", func(m *model) tea.Cmd { return m.taskScreen(st.ActiveTaskID) }}}, s.rows...)
		}
		notices, err := getAll[notification.Notice](ctx, c, "/api/notifications?filter=active&assistant_id="+string(id))
		if err != nil {
			return manageScreen{}, err
		}
		slices.SortFunc(notices, func(a, b notification.Notice) int { return a.CreatedAt.Compare(b.CreatedAt) })
		if len(notices) > 0 {
			n := notices[0]
			s.body += "\n\n待处理提醒：" + noticeLabel(n)
			s.rows = append([]manageRow{{"查看当前提醒", func(m *model) tea.Cmd { return m.noticeScreen(n) }}, {fmt.Sprintf("关闭当前提醒（共 %d 条）", len(notices)), func(m *model) tea.Cmd {
				return m.confirmAction("关闭提醒", "关闭仅表示已确认，不改变任务或权限。", "关闭", func(ctx context.Context, c *runtimeclient.Client) error {
					return c.Do(ctx, "POST", "/api/notifications/update", map[string]any{"IDs": []core.ID{n.ID}, "Action": "dismiss"}, nil)
				}, "/status")
			}}}, s.rows...)
		}
		return s, nil
	})
}
func (m *model) noticeScreen(n notification.Notice) tea.Cmd {
	return m.loadScreen("提醒详情", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		if err := c.Do(ctx, "POST", "/api/notifications/update", map[string]any{"IDs": []core.ID{n.ID}, "Action": "read"}, nil); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "提醒详情", body: noticeLabel(n) + "\n" + n.Label}
		s.rows = []manageRow{{"打开相关内容", func(m *model) tea.Cmd {
			switch n.Source.Kind {
			case "task":
				return m.taskScreen(n.Source.ID)
			case "permission_request":
				return m.requestByIDScreen(n.Source.ID)
			case "schedule", "todo":
				return m.planScreen(n.Source)
			default:
				if strings.Contains(n.Kind, "channel") || strings.Contains(n.Kind, "weixin") {
					return m.channelScreen(n.AssistantID)
				}
				return m.statusScreen(n.AssistantID)
			}
		}}}
		return s, nil
	})
}
func (m *model) tasksScreen(id core.ID) tea.Cmd {
	return m.loadScreen("业务任务", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		items, err := getAll[task.Task](ctx, c, "/api/tasks?assistant_id="+string(id))
		if err != nil {
			return manageScreen{}, err
		}
		slices.SortStableFunc(items, func(a, b task.Task) int {
			active := func(t task.Task) bool {
				return t.State != task.Completed && t.State != task.Failed && t.State != task.Canceled
			}
			if active(a) != active(b) {
				if active(a) {
					return -1
				}
				return 1
			}
			return b.UpdatedAt.Compare(a.UpdatedAt)
		})
		s := manageScreen{title: "业务任务", body: "未结束任务优先；停止助理一轮不等于取消任务。"}
		for _, t := range items {
			s.rows = append(s.rows, manageRow{stateText(string(t.State)) + " · " + t.Goal, func(m *model) tea.Cmd { return m.taskScreen(t.ID) }})
		}
		if len(items) == 0 {
			s.body = "暂无业务任务。可以在聊天中交代目标。"
		}
		return s, nil
	})
}
func (m *model) taskScreen(id core.ID) tea.Cmd {
	return m.loadScreen("任务详情", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var t task.Task
		if err := c.Do(ctx, "GET", "/api/tasks/"+string(id), nil, &t); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "任务详情", body: fmt.Sprintf("%s\n状态：%s\n原因：%s\n费用：%s %s\n修订：%d", t.Goal, stateText(string(t.State)), reasonText(t.WaitingReason), moneyNumber(t.Spent), t.Spent.Currency, t.Revision)}
		if t.State != task.Completed && t.State != task.Failed && t.State != task.Canceled {
			action, label := "pause", "暂停任务"
			if t.State == task.Paused {
				action, label = "resume", "恢复任务"
			}
			for _, v := range []struct{ action, label string }{{action, label}, {"cancel", "取消任务"}} {
				op := mutation(t.Version)
				s.rows = append(s.rows, manageRow{v.label, func(m *model) tea.Cmd {
					return m.confirmAction(v.label, t.Goal+"\n已产生的文件与费用保留。", v.label, func(ctx context.Context, c *runtimeclient.Client) error {
						return c.Do(ctx, "POST", "/api/tasks/"+v.action, task.ControlRequest{Mutation: op, ID: id}, nil)
					}, "/tasks")
				}})
			}
		}
		if t.State != task.Running {
			op := mutation(t.Version)
			s.rows = append(s.rows, manageRow{"追加修订", func(m *model) tea.Cmd {
				return m.showScreen(manageScreen{title: "追加修订", body: t.Goal, fields: []formField{field("instructions", "修订说明", ""), field("criteria", "验收要求（分号分隔）", "")}, submitLabel: "提交修订", returnTo: "/tasks", submit: func(v map[string]string) (func(context.Context, *runtimeclient.Client) error, error) {
					if strings.TrimSpace(v["instructions"]) == "" {
						return nil, fmt.Errorf("请填写修订说明")
					}
					return func(ctx context.Context, c *runtimeclient.Client) error {
						return c.Do(ctx, "POST", "/api/tasks/revise", task.ReviseRequest{Mutation: op, ID: id, Instructions: v["instructions"], AcceptanceCriteria: splitValues(v["criteria"])}, nil)
					}, nil
				}})
			}})
		}
		for _, fileID := range t.ArtifactIDs {
			s.rows = append(s.rows, manageRow{"下载交付物 · " + string(fileID), func(m *model) tea.Cmd { return m.artifactScreen(fileID) }})
		}
		revisions, err := getAll[task.Revision](ctx, c, "/api/tasks/"+string(id)+"/revisions")
		if err != nil {
			return s, err
		}
		for _, r := range revisions {
			s.rows = append(s.rows, manageRow{fmt.Sprintf("修订 %d · %s", r.Number, r.Instructions), func(m *model) tea.Cmd {
				detail := manageScreen{title: "历史修订", body: r.Instructions + "\n\n" + strings.Join(r.AcceptanceCriteria, "\n")}
				for _, f := range r.ArtifactIDs {
					detail.rows = append(detail.rows, manageRow{"下载交付物 · " + string(f), func(m *model) tea.Cmd { return m.artifactScreen(f) }})
				}
				return m.showScreen(detail)
			}})
		}
		return s, nil
	})
}
func splitValues(s string) []string {
	var out []string
	for _, v := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == '；' || r == '\n' }) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func (m *model) searchForm(id core.ID, query string) tea.Cmd {
	if query != "" {
		return m.searchScreen(id, query)
	}
	s := manageScreen{title: "搜索聊天", fields: []formField{field("q", "关键词", ""), field("scope", "范围", "current", "current", "all")}, submitLabel: "搜索"}
	s.localSubmit = func(m *model, v map[string]string) tea.Cmd {
		if strings.TrimSpace(v["q"]) == "" {
			m.manage.screen.notice = "请填写关键词"
			return nil
		}
		target := id
		if v["scope"] == "all" {
			target = ""
		}
		return m.searchScreen(target, v["q"])
	}
	return m.showScreen(s)
}
func (m *model) searchScreen(id core.ID, q string) tea.Cmd {
	return m.loadScreen("搜索结果", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var results search.Results
		path := "/api/search?source=messages&limit=100&q=" + url.QueryEscape(q)
		if id != "" {
			path += "&assistant_id=" + string(id)
		}
		if err := c.Do(ctx, "GET", path, nil, &results); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "搜索结果", body: q}
		for _, h := range results.Hits.Items {
			s.rows = append(s.rows, manageRow{h.Title + " · " + h.Snippet, func(m *model) tea.Cmd { return m.messageScreen(h.AssistantID, h.Object.ID) }})
		}
		if len(s.rows) == 0 {
			s.body += "\n没有匹配的消息"
		}
		if results.Hits.NextCursor != "" {
			cursor := results.Hits.NextCursor
			s.rows = append(s.rows, manageRow{"下一页", func(m *model) tea.Cmd { return m.searchPage(path, cursor, q) }})
		}
		return s, nil
	})
}
func (m *model) searchPage(path, cursor, q string) tea.Cmd {
	return m.loadScreen("搜索结果", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var result search.Results
		if err := c.Do(ctx, "GET", path+"&cursor="+url.QueryEscape(cursor), nil, &result); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "搜索结果", body: q}
		for _, h := range result.Hits.Items {
			s.rows = append(s.rows, manageRow{h.Title + " · " + h.Snippet, func(m *model) tea.Cmd { return m.messageScreen(h.AssistantID, h.Object.ID) }})
		}
		if result.Hits.NextCursor != "" {
			s.rows = append(s.rows, manageRow{"下一页", func(m *model) tea.Cmd { return m.searchPage(path, result.Hits.NextCursor, q) }})
		}
		return s, nil
	})
}
func (m *model) messagesScreen(id core.ID) tea.Cmd {
	s := manageScreen{title: "选择消息", body: "包含当前已加载消息；更早消息先在聊天中上翻加载。"}
	if i := m.assistantIndex(id); i >= 0 {
		for j := len(m.assistants[i].messages) - 1; j >= 0; j-- {
			msg := m.assistants[i].messages[j]
			s.rows = append(s.rows, manageRow{msg.role + " · " + strings.ReplaceAll(msg.text, "\n", " "), func(m *model) tea.Cmd { return m.messageScreen(id, msg.id) }})
		}
	}
	return m.showScreen(s)
}
func (m *model) messageScreen(assistantID, id core.ID) tea.Cmd {
	return m.loadScreen("消息详情", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var a assistantapi.Assistant
		if err := c.Do(ctx, "GET", "/api/assistants/"+string(assistantID), nil, &a); err != nil {
			return manageScreen{}, err
		}
		var page core.Page[conversation.Message]
		if err := c.Do(ctx, "GET", "/api/conversations/"+string(a.ConversationID)+"/messages?message_id="+string(id), nil, &page); err != nil {
			return manageScreen{}, err
		}
		if len(page.Items) == 0 {
			return manageScreen{}, fmt.Errorf("消息不存在或已不可访问")
		}
		msg := page.Items[0]
		text := msg.Content.Text
		if msg.Content.Card != nil {
			text = msg.Content.Card.Title + "\n\n" + msg.Content.Card.Summary
		}
		s := manageScreen{title: "消息详情", body: fmt.Sprintf("%s · %s · %s\n\n%s", a.Name, msg.Sender, msg.CreatedAt.Local().Format(time.DateTime), text)}
		s.rows = []manageRow{{"复制完整原文", func(m *model) tea.Cmd {
			if m.writeClipboard == nil {
				m.manage.screen.notice = "当前终端未提供剪贴板"
			} else if err := m.writeClipboard(text); err != nil {
				m.manage.screen.notice = "复制请求失败"
			} else {
				m.manage.screen.notice = "复制请求已发送"
			}
			return nil
		}}, {"保存原文到本机", func(m *model) tea.Cmd { return m.saveTextScreen(text) }}}
		if card := msg.Content.Card; card != nil {
			s.rows = append(s.rows, manageRow{"打开关联详情", func(m *model) tea.Cmd {
				switch card.Kind {
				case conversation.TaskCard:
					return m.taskScreen(card.TargetID)
				case conversation.ScheduleCard:
					return m.planScreen(core.Object{Kind: "schedule", ID: card.TargetID})
				case conversation.PermissionRequestCard:
					return m.requestByIDScreen(card.TargetID)
				default:
					return m.resourceScreen(card.TargetID)
				}
			}})
		}
		for _, f := range msg.Content.AttachmentIDs {
			s.rows = append(s.rows, manageRow{"下载附件 · " + string(f), func(m *model) tea.Cmd { return m.artifactScreen(f) }})
		}
		if msg.Sender == conversation.Assistant {
			s.rows = append(s.rows, manageRow{"查看本轮用量", func(m *model) tea.Cmd { return m.metricsScreen(a.ConversationID, id) }})
		}
		return s, nil
	})
}
func (m *model) metricsScreen(chat, id core.ID) tea.Cmd {
	return m.loadScreen("本轮用量", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var v agent.TurnMetrics
		if err := c.Do(ctx, "GET", "/api/conversations/"+string(chat)+"/messages/"+string(id)+"/metrics", nil, &v); err != nil {
			return manageScreen{}, err
		}
		return manageScreen{title: "本轮用量", body: metricsText(v)}, nil
	})
}
func (m *model) channelScreen(id core.ID) tea.Cmd {
	return m.loadScreen("聊天入口", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var channel struct{ Channel string }
		if err := c.Do(ctx, "GET", "/api/assistants/"+string(id)+"/chat-channel", nil, &channel); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "聊天入口", body: "当前入口：" + channel.Channel}
		if slices.Contains([]string{"weixin", "telegram", "feishu", "slack", "dingtalk", "qq"}, channel.Channel) {
			s.rows = []manageRow{{"解绑并恢复 Molr 聊天", func(m *model) tea.Cmd {
				return m.confirmAction("解绑聊天入口", "外部渠道将停止收发，未完成投递会取消；聊天记录保留。", "解绑", func(ctx context.Context, c *runtimeclient.Client) error {
					return c.Do(ctx, "POST", "/api/"+channel.Channel+"/disconnect", map[string]any{"AssistantID": id}, nil)
				}, "/status")
			}}}
		}
		return s, nil
	})
}

func metricsText(v agent.TurnMetrics) string {
	body := fmt.Sprintf("状态：%s\n模型调用：%d\n工具调用：%d\n重试：%d", v.State, v.ModelCalls, v.ToolCalls, v.Retries)
	if v.Usage != nil {
		body += fmt.Sprintf("\n输入 tokens：%d\n输出 tokens：%d\n缓存 tokens：%d", v.Usage.InputTokens, v.Usage.OutputTokens, v.Usage.CachedTokens)
	}
	for _, cost := range v.Charges {
		body += "\n费用：" + moneyNumber(cost) + " " + cost.Currency
	}
	if !v.UsageComplete || !v.CostComplete {
		body += "\n用量或费用尚不完整"
	}
	return body
}

func (m *model) managementMenu() tea.Cmd {
	s := manageScreen{title: "操作与帮助", body: "↑↓ 选择，Enter 打开；Esc 返回聊天。"}
	for _, entry := range []struct{ name, label string }{{"/new", "创建助理"}, {"/settings", "当前助理设置"}, {"/status", "执行状态与提醒"}, {"/approval", "待审批申请"}, {"/permissions", "资源授权"}, {"/tasks", "业务任务与交付物"}, {"/plans", "后续计划"}, {"/processes", "后台进程与终端"}, {"/files", "工作区文件"}, {"/search", "搜索聊天"}, {"/messages", "消息详情与复制"}, {"/channel", "聊天入口"}} {
		s.rows = append(s.rows, manageRow{entry.label + " · " + entry.name, func(m *model) tea.Cmd { return m.openManagement(entry.name, "") }})
	}
	s.rows = append(s.rows, manageRow{"快捷键说明", func(m *model) tea.Cmd {
		return m.showScreen(manageScreen{title: "快捷键说明", body: helpMarkdown})
	}})
	return m.showScreen(s)
}

func noticeLabel(n notification.Notice) string {
	title := map[string]string{"permission.request": "有新的授权申请", "plan.ready": "计划已可交接", "task.failed": "任务执行失败", "execution.failed": "后台执行失败", "assistant.waiting": "助理等待处理", "dream.failed": "记忆整理未完成"}[n.Kind]
	if title == "" {
		if strings.HasSuffix(n.Kind, ".connection") {
			title = "聊天入口连接异常"
		} else if strings.HasSuffix(n.Kind, ".delivery") {
			title = "渠道消息投递异常"
		} else {
			title = "有一条待处理提醒"
		}
	}
	if n.Code != "" {
		title += "：" + reasonText(n.Code)
	}
	return title
}

func (m *model) pendingScreen(id core.ID, p pendingSend) tea.Cmd {
	return m.showScreen(manageScreen{title: "上次未确认发送", body: p.text + fmt.Sprintf("\n\n附件：%d 个。继续使用原载荷与幂等键；不会发送后来的编辑。", len(p.attachments)), rows: []manageRow{{"继续发送并确认结果", func(m *model) tea.Cmd {
		if m.assistantID() != id || m.assistants[m.selected].pending.key != p.key {
			m.manage.screen.notice = "发送状态已变化，请返回重新查看"
			return nil
		}
		m.manage.stack = nil
		m.back = nil
		m.page = chatPage
		focus := m.input.Focus()
		next, cmd := m.sendPending()
		*m = next.(model)
		return tea.Batch(focus, cmd)
	}}}})
}
