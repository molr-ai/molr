package tui

import (
	"molr.ai/runtime/internal/policy"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type page uint8

const (
	chatPage page = iota
	assistantsPage
	approvalPage
	helpPage
)

type replyMsg struct {
	assistant  int
	generation uint64
	text       string
}

type model struct {
	restoreHistory func(string) string
	connectionName string
	live           *liveSession
	review         *policy.PermissionRequest
	reviewKey      string
	reviewDecision int
	resolving      bool
	approvalScroll int
	width, height  int
	assistants     []assistant
	selected       int
	page           page
	back           []page
	input          textarea.Model
	search         textinput.Model
	pickerCursor   int
	approvalChoice int // 0 approves, 1 rejects; opening the page defaults to approve.
	viewport       viewport.Model
	help           viewport.Model
	markdown       markdownRenderer
	notice         string
	selection      *textSelection
	selectionID    uint64
	writeClipboard func(string) error
}

func newComposer() textarea.Model {
	input := textarea.New()
	input.Placeholder = "发消息，或输入 /help"
	input.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "› "
		}
		return "  "
	})
	input.ShowLineNumbers = false
	input.CharLimit = 4000
	input.SetHeight(1)
	input.FocusedStyle.Base = lipgloss.NewStyle()
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.Text = lipgloss.NewStyle()
	input.FocusedStyle.Placeholder = muted
	input.FocusedStyle.Prompt = accent
	input.BlurredStyle = input.FocusedStyle
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	// Bracketed paste is handled by the terminal, without reading the OS clipboard.
	input.KeyMap.Paste = key.NewBinding(key.WithDisabled())
	return input
}

func newModel() model {
	assistants := demoAssistants()
	for i := range assistants {
		assistants[i].composer = newComposer()
	}
	search := textinput.New()
	search.Placeholder = "搜索助理…"
	search.Prompt = "› "
	search.PromptStyle = accent
	search.PlaceholderStyle = muted
	search.CharLimit = 100
	search.KeyMap.Paste = key.NewBinding(key.WithDisabled())
	m := model{assistants: assistants, input: assistants[0].composer, search: search,
		viewport: viewport.New(60, 12), help: viewport.New(60, 12)}
	m.input.Focus()
	m.resize(100, 30)
	return m
}

func (m model) Init() tea.Cmd {
	if m.live != nil {
		return tea.Batch(textarea.Blink, m.waitLive(), m.loadLive(false))
	}
	return textarea.Blink
}

func (m model) Update(event tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.updateLive(event); handled {
		return next, cmd
	}
	if key, ok := event.(tea.KeyMsg); ok && m.selection != nil {
		switch key.String() {
		case "ctrl+c":
			return m, m.copySelection()
		case "ctrl+q":
			return m, tea.Quit
		case "esc":
			cmd := m.clearSelection()
			return m, cmd
		default:
			focus := m.clearSelection()
			next, cmd := m.Update(event)
			return next, tea.Batch(focus, cmd)
		}
	}

	switch msg := event.(type) {
	case clipboardResult:
		if m.selection != nil && m.selection.id == msg.id {
			m.selection.status = "复制请求已发送"
			if msg.err != nil {
				m.selection.status = "复制失败，请重试"
			}
		}
		return m, nil
	case tea.WindowSizeMsg:
		cmd := m.clearSelection()
		m.resize(msg.Width, msg.Height)
		return m, cmd
	case tea.MouseMsg:
		if msg.Button != tea.MouseButtonWheelUp && msg.Button != tea.MouseButtonWheelDown {
			return m.selectWithMouse(msg)
		}
		focus := m.clearSelection()
		var cmd tea.Cmd
		switch m.page {
		case chatPage:
			m.viewport, cmd = m.viewport.Update(msg)
			if m.viewport.AtBottom() && len(m.assistants) > 0 {
				m.assistants[m.selected].unread = false
			}
		case helpPage:
			m.help, cmd = m.help.Update(msg)
		case assistantsPage:
			delta := 1
			if msg.Button == tea.MouseButtonWheelUp {
				delta = -1
			}
			m.pickerCursor = max(0, min(len(m.matches())-1, m.pickerCursor+delta))
		}
		if m.live != nil && m.page == chatPage {
			if m.viewport.AtTop() && len(m.assistants) > 0 && m.assistants[m.selected].older != "" && !m.live.loading {
				cmd = tea.Batch(cmd, m.loadLive(true))
			}
			cmd = tea.Batch(cmd, m.markLiveRead())
		}
		return m, tea.Batch(focus, cmd)
	case replyMsg:
		if msg.assistant < 0 || msg.assistant >= len(m.assistants) {
			return m, nil
		}
		a := &m.assistants[msg.assistant]
		if a.generation != msg.generation || !a.busy {
			return m, nil
		}
		a.busy = false
		a.messages = append(a.messages, message{role: "assistant", text: msg.text})
		a.unread = msg.assistant != m.selected || m.page != chatPage || m.selection != nil || !m.viewport.AtBottom()
		if msg.assistant == m.selected {
			m.refresh(m.selection == nil && m.page == chatPage && m.viewport.AtBottom())
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			return m, tea.Quit
		case "esc":
			cmd := m.goBack()
			return m, cmd

		case "ctrl+r":
			if m.live != nil {
				m.notice = "正在加载…"
				return m, m.scheduleLive()
			}
		case "f1":
			if m.page == helpPage {
				cmd := m.goBack()
				return m, cmd
			}
			cmd := m.openPage(helpPage)
			return m, cmd
		}
		if m.width < 42 || m.height < 18 {
			return m, nil
		}
		if m.page == chatPage {
			switch msg.String() {
			case "ctrl+k":
				cmd := m.openPage(assistantsPage)
				return m, cmd
			case "ctrl+p":
				cmd := m.openPage(approvalPage)
				return m, cmd
			case "pgup":
				if m.live != nil && m.viewport.AtTop() && len(m.assistants) > 0 && m.assistants[m.selected].older != "" {
					return m, m.loadLive(true)
				}
				m.viewport.HalfPageUp()
				return m, nil
			case "pgdown":
				m.viewport.HalfPageDown()
				if m.viewport.AtBottom() && len(m.assistants) > 0 {
					m.assistants[m.selected].unread = false
				}
				return m, m.markLiveRead()
			case "ctrl+end":
				m.viewport.GotoBottom()
				if len(m.assistants) > 0 {
					m.assistants[m.selected].unread = false
				}
				return m, m.markLiveRead()
			case "enter":
				return m.send()
			}
		}
		if m.page == approvalPage {
			return m.updateApproval(msg)
		}
	}
	var cmd tea.Cmd
	switch m.page {
	case chatPage:
		m.input, cmd = m.input.Update(event)
		m.resizeComposer()
	case assistantsPage:
		return m.updatePicker(event)
	case helpPage:
		m.help, cmd = m.help.Update(event)
	}
	return m, cmd
}

func (m model) send() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(cleanText(m.input.Value()))
	if text == "" {
		return m, nil
	}
	switch text {
	case "/help", "/assistant", "/approval":
		m.input.Reset()
		m.resizeComposer()
		target := helpPage
		if text == "/assistant" {
			target = assistantsPage
		}
		if text == "/approval" {
			target = approvalPage
		}
		cmd := m.openPage(target)
		return m, cmd
	case "/quit":
		return m, tea.Quit
	}
	if m.live != nil {
		return m.sendLive(text)
	}
	a := &m.assistants[m.selected]
	if a.busy {
		return m, nil
	}
	a.messages = append(a.messages, message{role: "user", text: text})
	if len(a.messages) > 100 {
		a.messages = a.messages[len(a.messages)-100:]
	}
	a.busy = true
	a.generation++
	m.input.Reset()
	m.resizeComposer()
	m.notice = ""
	m.refresh(true)
	result := replyMsg{assistant: m.selected, generation: a.generation, text: demoReply(text)}
	return m, tea.Tick(900*time.Millisecond, func(time.Time) tea.Msg { return result })
}

func (m *model) selectAssistant(index int) {
	if index == m.selected {
		return
	}
	previous := &m.assistants[m.selected]
	previous.composer = m.input
	previous.scroll, previous.follow, previous.visited = m.viewport.YOffset, m.viewport.AtBottom(), true
	m.selected = index
	a := &m.assistants[index]
	m.input = a.composer
	m.input.SetWidth(m.contentWidth())
	m.resizeComposer()
	m.notice = ""
	m.refresh(!a.visited || a.follow)
	if a.visited && !a.follow {
		m.viewport.SetYOffset(a.scroll)
	}
	if m.viewport.AtBottom() {
		a.unread = false
	}
}

// Only plain text enters the terminal renderer, including bracketed paste.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == '\u202e' || r == '\u202d' {
			return -1
		}
		return r
	}, s)
}
