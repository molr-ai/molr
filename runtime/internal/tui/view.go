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

func (m *model) refresh(follow bool) {
	m.resizeComposer()
	var parts []string
	var messages []message
	if len(m.assistants) > 0 {
		messages = m.assistants[m.selected].messages
	}
	for _, msg := range messages {
		text := cleanText(msg.text)
		for _, id := range msg.attachments {
			label := "附件 " + string(id)
			if info, ok := m.fileCache[id]; ok {
				label = fmt.Sprintf("%s · %d 字节", cleanText(info.Name), info.Size)
			}
			text += "\n\n[" + label + " · /attachments 下载]"
		}
		switch msg.role {
		case "user":
			text = m.userBlock(m.markdown.render(text))
		case "system":
			text = muted.Render(ansi.Hardwrap("· "+text, m.contentWidth(), true))
			text = m.pagePadding(text)
		default:
			text = m.pagePadding(m.markdown.render(text))
		}
		parts = append(parts, text)
	}
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
	case helpPage:
		title, body = strong.Render("快捷键与命令"), m.help.View()
		left, right = "↑↓ / PgUp / PgDn 滚动", "Esc 返回"
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
		if m.page == approvalPage {
			left, right = "↑↓ 选择 · Enter 确认", "PgUp/PgDn 滚动 · Esc 返回"
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

func (m model) chatView() string {
	var a assistant
	if len(m.assistants) > 0 {
		a = m.assistants[m.selected]
	}
	status := ""
	switch {
	case m.notice != "":
		status = muted.Render(m.notice)
	case a.busy:
		status = accent.Render("正在生成演示回复…")
		if m.live != nil {
			status = accent.Render("正在处理…")
		}
	case a.approval == "pending":
		status = warning.Render("待审批 · 读取 weekly.md · Ctrl+P 查看")
		if m.live != nil {
			status = warning.Render("待审批 · Ctrl+P 查看")
		}
	case !m.viewport.AtBottom():
		status = muted.Render("正在查看历史 · Ctrl+End 回到最新消息")
	}
	if m.live != nil && m.notice == "" && a.approval != "pending" {
		switch a.status.State {
		case "waiting":
			status = muted.Render("等待中 · " + cleanText(a.status.WaitingReason))
		case "dreaming":
			status = muted.Render("正在整理记忆…")
		case "paused", "stopped":
			status = warning.Render("助理已暂停")
		}
		if a.status.ModelRetry != nil {
			status = muted.Render("模型请求重试中…")
		}
	}
	if m.live != nil && len(m.assistants) == 0 && !m.live.loading && m.notice == "" {
		status = muted.Render("尚无助理，请在 WebUI 创建后按 Ctrl+R 刷新")
	}
	var attachmentRows []string
	for i, d := range a.attachments {
		if i == m.attachmentRows()-1 && len(a.attachments) > m.attachmentRows() {
			attachmentRows = append(attachmentRows, muted.Render(fmt.Sprintf("还有 %d 个附件 · Ctrl+E 查看", len(a.attachments)-i)))
			break
		}
		attachmentRows = append(attachmentRows, muted.Render(cellTruncate(attachmentLabel(d), m.contentWidth(), "…")))
	}
	composerBody := m.composerView()
	if notice := m.channelNotice(); notice != "" {
		status = warning.Render(notice)
		if !strings.HasPrefix(m.input.Value(), "/") {
			if a.channel == "" {
				composerBody = muted.Render("正在确认聊天入口…\n草稿已保留\nCtrl+R 刷新 · Ctrl+O 连接")
			} else {
				composerBody = muted.Render("聊天输入暂不可用，草稿已保留\nWebUI → 切换聊天入口 → 解绑\n/help 查看管理指令 · Ctrl+R 刷新")
			}
		}
	}
	if matches := m.commandMatches(); len(matches) > 0 {
		index := min(m.commandCursor, len(matches)-1)
		status = accent.Render(matches[index] + " · ↑↓ 选择 · Tab/Enter 补全")
	}
	if a.status.Compression != nil && a.status.Compression.State == "running" {
		status = accent.Render("正在压缩上下文…")
	}
	if a.status.Expedite != nil && (a.status.Expedite.State == "pending" || a.status.Expedite.State == "waiting_tool" || a.status.Expedite.State == "waiting") {
		status = accent.Render("已加急，等待读取补充消息")
	}
	if len(attachmentRows) > 0 {
		composerBody = strings.Join(attachmentRows, "\n") + "\n" + composerBody
	}
	composer := m.userBlock(composerBody)
	history := m.viewport.View()
	if m.restoreHistory != nil {
		history = m.restoreHistory(history)
	}
	return history + "\n\n" + m.pagePadding(cellTruncate(status, m.contentWidth(), "…")) + "\n\n" + composer
}

func (m model) userBlock(content string) string {
	// Markdown and cursor spans reset ANSI styles. Restore the shared surface so
	// user text, blank lines and padding retain one continuous background.
	restore := strings.TrimSuffix(userSurface.Render(""), "\x1b[0m")
	content = strings.NewReplacer("\x1b[0m", "\x1b[0m"+restore, "\x1b[m", "\x1b[m"+restore).Replace(content)
	masked, restoreKeycaps := layoutKeycaps(content)
	return restoreKeycaps(userSurface.Width(m.width).Padding(1, 2).Render(masked))
}
