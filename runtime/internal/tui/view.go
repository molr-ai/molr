package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Shared terminal roles adapt user-authored content to light and dark terminals.
const accentColor lipgloss.Color = "6"

var (
	accent      = lipgloss.NewStyle().Foreground(accentColor)
	unreadBadge = accent.Bold(true).Reverse(true)
	muted       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warning     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	strong      = lipgloss.NewStyle().Bold(true)
	tableHeader = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "229"})
	selectedTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "0", Dark: "15"}).
				Background(lipgloss.AdaptiveColor{Light: "153", Dark: "24"})
	tableRule = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "252", Dark: "238"})
	quoteBar = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "12"})
	userSurface = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "0", Dark: "7"}).
			Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"})
)

func (m model) contentWidth() int { return max(1, m.width-4) }
func (m model) bodyHeight() int   { return max(1, m.height-6) }

func (m *model) resize(width, height int) {
	follow := m.viewport.AtBottom()
	m.width, m.height = width, height
	m.viewport.Width = max(1, width)
	m.input.SetWidth(m.contentWidth())
	m.resizeComposer()
	m.search.Width = max(1, m.contentWidth()-3)
	m.files.path.Width = max(1, m.contentWidth()-8)
	if m.files.preview != "" {
		m.files.preview = wrapPreview(string(m.files.previewData), m.contentWidth())
	}
	m.help.Width, m.help.Height = m.contentWidth(), m.bodyHeight()
	m.markdown = newMarkdownRenderer(m.contentWidth())
	m.help.SetContent(m.markdown.render(helpMarkdown))
	m.refresh(follow)
}

type messageRenderKey struct {
	role, text string
	width      int
}

func (m *model) refresh(follow bool) {
	m.resizeComposer()
	var parts []string
	var messages []message
	if len(m.assistants) > 0 {
		messages = m.assistants[m.selected].messages
	}
	cache := make(map[messageRenderKey]string, len(messages))
	for _, msg := range messages {
		text := cleanText(msg.text)
		for _, id := range msg.attachments {
			label := "附件 " + string(id)
			if info, ok := m.fileCache[id]; ok {
				label = fmt.Sprintf("%s · %d 字节", cleanText(info.Name), info.Size)
			}
			text += "\n\n[" + label + " · /attachments 下载]"
		}
		key := messageRenderKey{msg.role, text, m.width}
		if rendered, ok := m.renderCache[key]; ok {
			cache[key] = rendered
			parts = append(parts, rendered)
			continue
		}
		switch msg.role {
		case "user":
			text = m.userBlock(m.markdown.render(text))
		case "assistant":
			text = m.pagePadding(m.markdown.render(text))
			// Use the left gutter so the marker does not change Markdown wrapping.
			text = muted.Render("●") + " " + strings.TrimPrefix(text, "  ")
		case "system":
			text = muted.Render(ansi.Hardwrap("· "+text, m.contentWidth(), true))
			text = m.pagePadding(text)
		default:
			text = m.pagePadding(m.markdown.render(text))
		}
		cache[key] = text
		parts = append(parts, text)
	}
	m.renderCache = cache
	content, restore := layoutKeycaps(strings.Join(parts, "\n\n"))
	m.restoreHistory = restore
	m.viewport.SetContent(content)
	if follow {
		m.viewport.GotoBottom()
	}
}

func (m model) View() string {
	if m.selection != nil {
		return m.selection.render(m.width, m.height)
	}
	if m.width < 42 || m.height < 18 {
		return ansi.Hardwrap("Molr · 演示\n终端至少需要 42 列 × 18 行。\n请放大窗口；Ctrl+C 退出。", max(1, m.width), true)
	}
	var title, body, left, right string
	switch m.page {
	case chatPage:
		title = accent.Bold(true).Render("Molr")
		if len(m.assistants) > 0 {
			title += muted.Render(" / ") + strong.Render(m.assistants[m.selected].name)
		}
		body = m.chatView()
		left, right = "Enter 发送 · Alt+Enter 换行", "Ctrl+K 助理 · F1 帮助"
		if m.contentWidth() < 64 {
			left = "Enter 发送"
		}
	case assistantsPage:
		title, body = strong.Render("切换助理"), m.pickerView()
		left, right = "↑↓ 选择 · Enter 打开", "Esc 返回"
	case approvalPage:
		title, body = strong.Render("授权申请"), m.approvalView()
		left, right = "↑↓ 选择 · Enter 确认", "Esc 返回"
		if len(m.assistants) == 0 || m.assistants[m.selected].approval != "pending" {
			left = ""
		}
	case filesPage:
		title, body = strong.Render("添加附件"), m.filesView()
		left, right = "↑↓ 选择 · 空格多选 · Enter 添加", "Ctrl+L 路径 · Tab 补全 · Esc 返回"
	case attachmentsPage:
		title, body = strong.Render("聊天附件"), m.filesView()
		left, right = "↑↓ 选择 · Enter 下载", "Esc 返回"
		if m.files.drafts {
			title = strong.Render("待发送附件")
			left = "Enter 预览 · R 重试 · D 移除"
		}
	case managePage:
		title, body = strong.Render(m.manage.screen.title), m.managedView()
		left, right = "↑↓ 选择 · Enter 确认", "Tab 字段 · ←→ 选项 · Esc 返回"
		if m.manage.busy && m.manage.screen.submit != nil && !m.manage.screen.cancellable {
			left, right = "正在提交…", "Ctrl+Q 退出"
		}
	case helpPage:
		title, body = strong.Render("快捷键与命令"), m.help.View()
		left, right = "↑↓ / Alt+↑↓ 滚动", "Esc 返回"
	}
	badge := warning.Render("演示模式")
	if m.live != nil {
		if m.page == chatPage {
			right = "Ctrl+O 连接 · Ctrl+K 助理"
			if m.contentWidth() >= 64 {
				right += " · F1 帮助"
			}
		}
		badge = muted.Render("连接中…")
		if m.live.connected {
			badge = muted.Render("已连接")
		}
		if m.live.expired {
			badge = warning.Render("登录已过期")
		}
		if m.connectionName != "" {
			badge = muted.Render(cellTruncate(m.connectionName, max(4, m.contentWidth()/4), "…")+" · ") + badge
		}
	}
	header := spread(title, badge, m.contentWidth())
	if m.page != chatPage {
		body = m.pagePadding(body)
	}
	masked, restore := layoutKeycaps(body)
	body = restore(lipgloss.NewStyle().Width(m.width).Height(m.bodyHeight()).Render(masked))
	footer := muted.Render(spread(left, right, m.contentWidth()))
	return "\n" + m.pagePadding(header) + "\n\n" + body + "\n\n" + m.pagePadding(footer) + "\n"
}

func (m model) pagePadding(content string) string {
	masked, restore := layoutKeycaps(content)
	return restore(lipgloss.NewStyle().Width(m.width).Padding(0, 2).Render(masked))
}

func spread(left, right string, width int) string {
	left = cellTruncate(left, max(0, width-cellWidth(right)-1), "…")
	return left + strings.Repeat(" ", max(1, width-cellWidth(left)-cellWidth(right))) + right
}

// Keep the visible status and its action together so overrides cannot leave
// a hidden approval or other stale action behind.
func (m model) chatStatus() (string, string) {
	var a assistant
	if len(m.assistants) > 0 {
		a = m.assistants[m.selected]
	}
	status, action := "", ""
	switch {
	case m.notice != "":
		status, action = muted.Render(m.notice), ""
	case a.busy:
		status, action = accent.Render("正在生成演示回复…"), ""
		if m.live != nil {
			status, action = accent.Render("正在处理…"), ""
		}
	case a.approval == "pending":
		status, action = warning.Render("待审批 · 读取 weekly.md · Ctrl+P 查看"), "/approval"
		if m.live != nil {
			status, action = warning.Render("待审批 · Ctrl+P 查看"), "/approval"
		}
	}
	if m.live != nil && m.notice == "" && a.approval != "pending" {
		switch a.status.State {
		case "waiting":
			status, action = muted.Render("等待中 · "+cleanText(a.status.WaitingReason)), ""
		case "dreaming":
			status, action = muted.Render("正在整理记忆…"), ""
		case "paused", "stopped":
			status, action = warning.Render("助理已暂停"), ""
		}
		if a.status.ModelRetry != nil {
			status, action = muted.Render("模型请求重试中…"), ""
		}
	}
	if m.live != nil && len(m.assistants) == 0 && !m.live.loading && m.notice == "" {
		status, action = muted.Render("尚无助理，输入 /new 创建"), "/new"
	}
	if notice := m.channelNotice(); notice != "" && m.notice == "" {
		status, action = warning.Render(notice), ""
		if a.loaded && a.channel != "" {
			action = "/channel"
		}
	}
	if matches := m.commandMatches(); len(matches) > 0 {
		index := min(m.commandCursor, len(matches)-1)
		status, action = accent.Render(matches[index]+" · ↑↓ 选择 · Tab/Enter 补全"), ""
	}
	if a.status.Compression != nil && a.status.Compression.State == "running" {
		status, action = accent.Render("正在压缩上下文…"), ""
	}
	if a.status.Expedite != nil && (a.status.Expedite.State == "pending" || a.status.Expedite.State == "waiting_tool" || a.status.Expedite.State == "waiting") {
		status, action = accent.Render("已加急，等待读取补充消息"), ""
	}
	if len(a.notices) > 0 {
		n := a.notices[0]
		if m.notice == "" {
			status, action = warning.Render(fmt.Sprintf("提醒 %d 条：%s · /status 查看", len(a.notices), noticeLabel(n))), ""
		} else {
			status, action = warning.Render(fmt.Sprintf("提醒 %d · /status · ", len(a.notices)))+status, ""
		}
	}
	if len(a.notices) > 0 && m.live != nil {
		action = "/status"
	}
	if m.draftError != "" {
		status, action = warning.Render("草稿未保存到本机："+m.draftError), ""
	}
	label, _, _ := m.latestControl()
	width := m.contentWidth()
	if label != "" {
		width = max(0, width-cellWidth(label)-1)
	}
	return cellTruncate(status, width, "…"), action
}

func (m model) chatView() string {
	var a assistant
	if len(m.assistants) > 0 {
		a = m.assistants[m.selected]
	}
	var attachmentRows []string
	for i, d := range a.attachments {
		if m.commandOnly() {
			break
		}
		if i == m.attachmentRows()-1 && len(a.attachments) > m.attachmentRows() {
			attachmentRows = append(attachmentRows, muted.Render(fmt.Sprintf("还有 %d 个附件 · Ctrl+E 查看", len(a.attachments)-i)))
			break
		}
		attachmentRows = append(attachmentRows, muted.Render(cellTruncate(attachmentLabel(d), m.contentWidth(), "…")))
	}
	composerBody := m.composerView()
	if len(attachmentRows) > 0 {
		composerBody = strings.Join(attachmentRows, "\n") + "\n" + composerBody
	}
	composer := m.userBlock(composerBody)
	history := m.viewport.View()
	if m.restoreHistory != nil {
		history = m.restoreHistory(history)
	}
	label, _, _ := m.latestControl()
	status, _ := m.chatStatus()
	statusRow := status
	if label != "" {
		statusRow = spread(status, accent.Render(label), m.contentWidth())
	}
	return history + "\n\n" + m.pagePadding(statusRow) + "\n\n" + composer
}

func (m model) userBlock(content string) string {
	// Markdown and cursor spans reset ANSI styles. Restore the shared surface so
	// user text, blank lines and padding retain one continuous background.
	restore := strings.TrimSuffix(userSurface.Render(""), "\x1b[0m")
	content = strings.NewReplacer("\x1b[0m", "\x1b[0m"+restore, "\x1b[m", "\x1b[m"+restore).Replace(content)
	masked, restoreKeycaps := layoutKeycaps(content)
	return restoreKeycaps(userSurface.Width(m.width).Padding(1, 2).Render(masked))
}

// The control shares the status row, keeping a blank row before the composer.
// Rendering and mouse hit testing share terminal-cell coordinates.
func (m model) latestControl() (label string, x, y int) {
	if m.page != chatPage || m.width < 42 || m.height < 18 || m.viewport.AtBottom() {
		return "", 0, 0
	}
	label = "↓ 返回底部 · Ctrl+G"
	if len(m.assistants) > 0 && m.assistants[m.selected].unreadCount > 0 {
		label = "↓ 有新消息 · Ctrl+G"
	}
	return label, m.width - 2 - cellWidth(label), 3 + m.viewport.Height + 1
}
