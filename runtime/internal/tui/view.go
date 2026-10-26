package tui

import (
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
	m.help.Width, m.help.Height = m.contentWidth(), m.bodyHeight()
	m.markdown = newMarkdownRenderer(m.contentWidth())
	m.help.SetContent(m.markdown.render(helpMarkdown))
	m.refresh(follow)
}

func (m *model) refresh(follow bool) {
	var parts []string
	var messages []message
	if len(m.assistants) > 0 {
		messages = m.assistants[m.selected].messages
	}
	for _, msg := range messages {
		text := cleanText(msg.text)
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
	m.viewport.SetContent(strings.Join(parts, "\n\n"))
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
			badge = muted.Render(ansi.Truncate(m.connectionName, max(4, m.contentWidth()/4), "…")+" · ") + badge
		}
		if m.page == approvalPage {
			left, right = "↑↓ 选择 · Enter 确认", "PgUp/PgDn 滚动 · Esc 返回"
		}
	}
	header := spread(title, badge, m.contentWidth())
	if m.page != chatPage {
		body = m.pagePadding(body)
	}
	body = lipgloss.NewStyle().Width(m.width).Height(m.bodyHeight()).Render(body)
	footer := muted.Render(spread(left, right, m.contentWidth()))
	return "\n" + m.pagePadding(header) + "\n\n" + body + "\n\n" + m.pagePadding(footer) + "\n"
}

func (m model) pagePadding(content string) string {
	return lipgloss.NewStyle().Width(m.width).Padding(0, 2).Render(content)
}

func spread(left, right string, width int) string {
	left = ansi.Truncate(left, max(0, width-lipgloss.Width(right)-1), "…")
	return left + strings.Repeat(" ", max(1, width-lipgloss.Width(left)-lipgloss.Width(right))) + right
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
	composer := m.userBlock(m.input.View())
	return m.viewport.View() + "\n\n" + m.pagePadding(ansi.Truncate(status, m.contentWidth(), "…")) + "\n\n" + composer
}

func (m model) userBlock(content string) string {
	// Markdown and cursor spans reset ANSI styles. Restore the shared surface so
	// user text, blank lines and padding retain one continuous background.
	restore := strings.TrimSuffix(userSurface.Render(""), "\x1b[0m")
	content = strings.NewReplacer("\x1b[0m", "\x1b[0m"+restore, "\x1b[m", "\x1b[m"+restore).Replace(content)
	return userSurface.Width(m.width).Padding(1, 2).Render(content)
}
