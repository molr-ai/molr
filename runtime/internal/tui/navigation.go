package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) openPage(next page) tea.Cmd {
	if next == helpPage && m.live != nil {
		return m.managementMenu()
	}
	if m.page == chatPage && len(m.assistants) > 0 {
		m.assistants[m.selected].follow = m.viewport.AtBottom()
	}
	m.input.Blur()
	m.search.Blur()
	m.back = append(m.back, m.page)
	m.page = next
	switch next {
	case assistantsPage:
		m.search.Reset()
		m.pickerCursor = m.selected
	case approvalPage:
		m.approvalChoice = 0
		m.approvalScroll = 0
		if m.live != nil {
			m.notice = ""
			m.review = nil
			m.reviewKey = ""
			m.resolving = false
			if len(m.assistants) > 0 && len(m.assistants[m.selected].requests) > 0 {
				q := m.assistants[m.selected].requests[0]
				m.review = &q
			}
		}
	case helpPage:
		m.help.GotoTop()
	}
	return m.focusPage()
}

func (m *model) goBack() tea.Cmd {
	if len(m.back) == 0 {
		return nil
	}
	m.search.Blur()
	m.page = m.back[len(m.back)-1]
	m.back = m.back[:len(m.back)-1]
	cmd := m.focusPage()
	if m.page == chatPage && len(m.assistants) > 0 && m.selection == nil {
		if m.assistants[m.selected].follow {
			m.refresh(true)
		}
		if m.live == nil && m.viewport.AtBottom() {
			m.assistants[m.selected].unreadCount = 0
		}
		cmd = tea.Batch(cmd, m.markLiveRead())
	}
	return cmd
}

func (m *model) focusPage() tea.Cmd {
	switch m.page {
	case chatPage:
		return m.input.Focus()
	case assistantsPage:
		return m.search.Focus()
	}
	return nil
}

func (m model) matches() []int {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	var matches []int
	for i, a := range m.assistants {
		if strings.Contains(strings.ToLower(a.name), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

func (m model) updatePicker(event tea.Msg) (tea.Model, tea.Cmd) {
	matches := m.matches()
	if msg, ok := event.(tea.KeyMsg); ok {
		switch msg.String() {
		case "up", "down":
			if len(matches) > 0 {
				delta := 1
				if msg.String() == "up" {
					delta = -1
				}
				m.pickerCursor = (m.pickerCursor + delta + len(matches)) % len(matches)
			}
			return m, nil
		case "ctrl+n":
			return m, m.assistantForm(true)
		case "enter":
			if len(matches) == 0 {
				return m, nil
			}
			m.selectAssistant(matches[m.pickerCursor])
			cmd := m.goBack()
			if m.live != nil {
				m.notice = "正在加载…"
				cmd = tea.Batch(cmd, m.scheduleLive())
			}
			return m, cmd
		}
	}
	previous := m.search.Value()
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(event)
	if previous != m.search.Value() {
		m.pickerCursor = 0
	}
	return m, cmd
}

func (m model) updateApproval(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.live != nil {
		return m.updateLiveApproval(msg)
	}
	a := &m.assistants[m.selected]
	if a.approval != "pending" {
		return m, nil
	}
	switch msg.String() {
	case "down", "tab", "up", "shift+tab":
		m.approvalChoice = (m.approvalChoice + 1) % 2
	case "enter":
		a.approval = "rejected"
		text := "已拒绝模拟申请；未读取文件。"
		if m.approvalChoice == 0 {
			a.approval = "approved"
			text = "已批准模拟申请；未授予真实权限。"
		}
		a.messages = append(a.messages, message{role: "system", text: text})
		// Keep the reader's previous history position; show the result near input too.
		m.refresh(m.viewport.AtBottom())
		m.notice = text
		cmd := m.goBack()
		return m, cmd
	}
	return m, nil
}
