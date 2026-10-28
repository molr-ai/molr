package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m model) pickerView() string {
	lines := []string{m.searchView(), "", muted.Render("助理"), ""}
	matches := m.matches()
	if len(matches) == 0 {
		return strings.Join(append(lines, "没有匹配的助理", muted.Render("Ctrl+N 创建助理 · Esc 返回聊天")), "\n")
	}
	rowHeight := 2
	if m.contentWidth() < 64 {
		rowHeight = 3
	}
	visible := max(1, (m.bodyHeight()-4)/rowHeight)
	start := max(0, min(m.pickerCursor-visible+1, len(matches)-visible))
	for pos := start; pos < min(len(matches), start+visible); pos++ {
		index := matches[pos]
		a := m.assistants[index]
		prefix := "  "
		if pos == m.pickerCursor {
			prefix = "› "
		}
		name := prefix + a.name
		if rowHeight == 2 {
			name += "   " + muted.Render(a.role)
		}
		status := ""
		switch {
		case a.busy:
			status = "正在处理"
		case a.approval == "pending":
			status = "待审批"
		case a.unread:
			status = "新消息"
		case index == m.selected:
			status = "当前"
		}
		if pos == m.pickerCursor {
			name = accent.Bold(true).Render(name)
		}
		lines = append(lines, spread(name, muted.Render(status), m.contentWidth()))
		if rowHeight == 3 {
			lines = append(lines, "  "+muted.Render(a.role))
		}
		lines = append(lines, "")
	}
	if m.live != nil {
		lines = append(lines, muted.Render("Ctrl+N 创建助理"))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func (m model) searchView() string {
	if m.search.Value() != "" {
		return m.search.View()
	}
	// Bubbles 0.21 indexes its placeholder by cell width as if it were runes.
	// Render the CJK placeholder here to avoid emitting padding NUL characters.
	placeholder := []rune(m.search.Placeholder)
	cursor := m.search.Cursor
	cursor.TextStyle = muted
	cursor.SetChar(string(placeholder[0]))
	return accent.Render(m.search.Prompt) + cursor.View() + muted.Render(string(placeholder[1:]))
}

func (m model) approvalView() string {
	if m.live != nil {
		return m.liveApprovalView()
	}
	a := m.assistants[m.selected]
	switch a.approval {
	case "approved":
		return "这条模拟申请已批准。\n\n" + muted.Render("没有授予真实权限，也没有读取文件。")
	case "rejected":
		return "这条模拟申请已拒绝。"
	case "pending":
		lines := []string{a.name + "希望读取一份报告。", "",
			muted.Render("资源     ") + "演示工作区",
			muted.Render("操作     ") + "读取",
			muted.Render("范围     ") + "/reports/weekly.md",
			muted.Render("有效期   ") + "本次演示", "",
			muted.Render("演示申请，不会读取真实文件。"), ""}
		for i, label := range []string{"批准", "拒绝"} {
			line := "  " + label
			if m.approvalChoice == i {
				line = accent.Bold(true).Render("› " + label)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	default:
		return ansi.Hardwrap("当前助理没有待处理的申请。\n\n可以返回聊天，按 Ctrl+K 切换到研究助理体验模拟审批。", m.contentWidth(), true)
	}
}
