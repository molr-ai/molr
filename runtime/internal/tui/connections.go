package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
)

// ConnectionOptions selects startup behavior and the private connection store.
type ConnectionOptions struct {
	Store      *runtimeclient.Connections
	Initial    string
	ForceLogin bool
	Username   string
}

type connectionPage uint8

const (
	connectionList connectionPage = iota
	connectionAdd
	connectionLogin
	connectionChat
	connectionQuitting
)

type connectionResult struct {
	attempt   uint64
	client    *runtimeclient.Client
	owner     core.ID
	needLogin bool
	challenge string
	signed    bool
	err       error
}
type chatResult struct {
	generation uint64
	msg        tea.Msg
}
type cachedChat struct {
	owner core.ID
	chat  model
}

type connectionModel struct {
	ctx            context.Context
	options        ConnectionOptions
	page           connectionPage
	width, height  int
	cursor, field  int
	fields         []textinput.Model
	notice         string
	busy           bool
	target         runtimeclient.Connection
	client         *runtimeclient.Client
	challenge      string
	attempt        uint64
	attemptContext context.Context
	cancelAttempt  context.CancelFunc
	generation     uint64
	chat           *model
	active         runtimeclient.Connection
	owner          core.ID
	cancelChat     context.CancelFunc
	cache          map[string]cachedChat
	clipboard      func(string) error
}

func newConnectionModel(ctx context.Context, options ConnectionOptions, clipboard func(string) error) connectionModel {
	return connectionModel{ctx: ctx, options: options, width: 100, height: 30, cache: map[string]cachedChat{}, clipboard: clipboard}
}
func (m connectionModel) Init() tea.Cmd {
	for _, c := range m.options.Store.Items {
		if c.ID == m.options.Initial {
			return func() tea.Msg { return openConnection{c} }
		}
	}
	return nil
}

type openConnection struct{ connection runtimeclient.Connection }

func tagChat(cmd tea.Cmd, generation uint64) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			out := make(tea.BatchMsg, 0, len(batch))
			for _, cmd := range batch {
				if cmd != nil {
					out = append(out, tagChat(cmd, generation))
				}
			}
			return out
		}
		return chatResult{generation, msg}
	}
}
func (m *connectionModel) chatUpdate(msg tea.Msg) tea.Cmd {
	if m.chat == nil {
		return nil
	}
	next, cmd := m.chat.Update(msg)
	chat := next.(model)
	m.chat = &chat
	return tagChat(cmd, m.generation)
}
func (m *connectionModel) manager() {
	m.cancelLogin()
	m.page = connectionList
	m.notice = ""
	m.cursor = 0
	for i, c := range m.options.Store.Items {
		if c.ID == m.active.ID {
			m.cursor = i
		}
	}
	if m.chat != nil {
		m.chat.clearSelection()
		m.chat.input.Blur()
		m.chat.search.Blur()
		m.chat.live.hidden = true
	}
}
func (m *connectionModel) cancelLogin() {
	if m.cancelAttempt != nil {
		m.cancelAttempt()
		m.cancelAttempt = nil
	}
	m.attempt++
	m.busy = false
	m.client = nil
	m.challenge = ""
	for i := range m.fields {
		m.fields[i].Reset()
		m.fields[i].Blur()
	}
	m.fields = nil
}
func (m *connectionModel) begin(c runtimeclient.Connection) tea.Cmd {
	if c.ID == m.active.ID && m.chat != nil && !m.chat.live.expired && !m.options.ForceLogin {
		m.page = connectionChat
		m.chat.live.hidden = false
		return tea.Batch(tagChat(m.chat.focusPage(), m.generation), tagChat(m.chat.markLiveRead(), m.generation))
	}
	m.cancelLogin()
	for i, item := range m.options.Store.Items {
		if item.ID == c.ID {
			m.cursor = i
		}
	}
	m.target = c
	m.busy = true
	m.notice = "正在连接…"
	m.page = connectionList
	ctx, cancel := context.WithCancel(m.ctx)
	m.attemptContext, m.cancelAttempt = ctx, cancel
	attempt, force, store := m.attempt, m.options.ForceLogin, m.options.Store
	m.options.ForceLogin = false
	return func() tea.Msg {
		out := connectionResult{attempt: attempt}
		client, err := runtimeclient.New(c.Server, c.CAFile)
		out.client = client
		out.err = err
		if err != nil {
			return out
		}
		restored, err := client.Restore(store.SessionPath(c))
		out.err = err
		if err != nil {
			return out
		}
		if !restored || force {
			out.needLogin = true
			return out
		}
		var principal core.Principal
		out.err = client.Do(ctx, "GET", "/api/auth/me", nil, &principal)
		var apiErr *runtimeclient.Error
		if errors.As(out.err, &apiErr) && apiErr.Status == 401 {
			out.needLogin = true
			out.err = nil
		}
		out.owner = principal.OwnerID
		return out
	}
}
func connectionFields(count int, width int) []textinput.Model {
	fields := make([]textinput.Model, count)
	for i := range fields {
		fields[i] = textinput.New()
		fields[i].Prompt = "› "
		fields[i].PromptStyle = accent
		fields[i].CharLimit = 2048
		fields[i].Width = max(1, width-8)
		fields[i].KeyMap.Paste = key.NewBinding(key.WithDisabled())
	}
	return fields
}
func (m *connectionModel) focusField() tea.Cmd {
	for i := range m.fields {
		m.fields[i].Blur()
	}
	return m.fields[m.field].Focus()
}
func (m *connectionModel) showLogin() tea.Cmd {
	m.page = connectionLogin
	m.notice = ""
	m.field = 0
	if m.challenge != "" {
		m.fields = connectionFields(1, m.width)
		m.fields[0].EchoMode = textinput.EchoPassword
		m.fields[0].EchoCharacter = '•'
	} else {
		m.fields = connectionFields(2, m.width)
		m.fields[0].SetValue(m.options.Username)
		m.fields[1].EchoMode = textinput.EchoPassword
		m.fields[1].EchoCharacter = '•'
		if m.options.Username != "" {
			m.field = 1
		}
	}
	return m.focusField()
}
func (m *connectionModel) login() tea.Cmd {
	if m.client == nil || m.busy {
		return nil
	}
	client, ctx, attempt, challenge := m.client, m.attemptContext, m.attempt, m.challenge
	username, password, code := "", "", ""
	if challenge == "" {
		username, password = strings.TrimSpace(m.fields[0].Value()), m.fields[1].Value()
		if username == "" {
			m.field = 0
			return m.focusField()
		}
		if password == "" {
			m.field = 1
			return m.focusField()
		}
		m.fields[1].Reset()
	} else {
		code = strings.TrimSpace(m.fields[0].Value())
		if code == "" {
			return nil
		}
		m.fields[0].Reset()
	}
	m.busy = true
	m.notice = "正在登录…"
	return func() tea.Msg {
		out := connectionResult{attempt: attempt, client: client, signed: true}
		if challenge == "" {
			out.challenge, out.err = client.Login(ctx, username, password)
		} else {
			out.err = client.CompleteMFA(ctx, challenge, code)
		}
		if out.err == nil && out.challenge == "" {
			var principal core.Principal
			out.err = client.Do(ctx, "GET", "/api/auth/me", nil, &principal)
			out.owner = principal.OwnerID
		}
		return out
	}
}
func (m *connectionModel) activate(result connectionResult) tea.Cmd {
	if result.owner == "" {
		m.notice = "登录响应缺少账号身份"
		m.page = connectionList
		return nil
	}
	if result.signed {
		if err := result.client.Save(m.options.Store.SessionPath(m.target)); err != nil {
			m.notice = "保存登录会话失败：" + cleanText(err.Error())
			m.page = connectionList
			return nil
		}
	}
	if err := m.options.Store.Select(m.target.ID); err != nil {
		m.notice = "保存连接失败：" + cleanText(err.Error())
		m.page = connectionList
		return nil
	}
	if m.chat != nil {
		// Pending mutations retain their keys for safe retry when returning.
		for i := range m.chat.assistants {
			m.chat.assistants[i].pending.active = false
			m.chat.assistants[i].commandBusy = false
			for j := range m.chat.assistants[i].attachments {
				d := &m.chat.assistants[i].attachments[j]
				if d.cancel != nil {
					d.cancel()
				}
				if d.busy {
					d.busy = false
					d.err = "上传已中断，请重试"
				}
				d.cancel = nil
			}
			m.chat.assistants[i].reading = false
		}
		if m.chat.files.cancel != nil {
			m.chat.files.cancel()
		}
		m.chat.files = fileDialog{}
		m.chat.resolving = false
		m.chat.review = nil
		m.chat.page = chatPage
		m.chat.back = nil
		m.cache[m.active.ID] = cachedChat{owner: m.owner, chat: *m.chat}
	}
	if m.cancelChat != nil {
		m.cancelChat()
	}
	m.generation++
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelChat = cancel
	events := watchClient(ctx, result.client)
	chat := newConnectedModel(ctx, result.client, events)
	if cached, ok := m.cache[m.target.ID]; ok && cached.owner == result.owner {
		chat = cached.chat
		chat.live = &liveSession{ctx: ctx, client: result.client, events: events}
		chat.notice = "正在连接…"
		chat.selection = nil
	}
	chat.connectionName = cleanText(m.target.Name)
	chat.resize(m.width, m.height)
	chat.writeClipboard = m.clipboard
	m.chat = &chat
	m.active = m.target
	m.owner = result.owner
	m.page = connectionChat
	m.notice = ""
	m.client = nil
	m.challenge = ""
	for i := range m.fields {
		m.fields[i].Reset()
	}
	m.fields = nil
	return tea.Batch(tagChat(chat.Init(), m.generation), tagChat(chat.focusPage(), m.generation))
}

func (m connectionModel) Update(event tea.Msg) (tea.Model, tea.Cmd) {
	if m.page == connectionQuitting {
		return m, nil
	}
	switch msg := event.(type) {
	case openConnection:
		cmd := m.begin(msg.connection)
		return m, cmd
	case connectionResult:
		if msg.attempt != m.attempt {
			return m, nil
		}
		m.busy = false
		if msg.err != nil {
			m.notice = cleanText(msg.err.Error())
			if m.page != connectionLogin {
				m.page = connectionList
			}
			return m, nil
		}
		m.client = msg.client
		if msg.challenge != "" {
			m.challenge = msg.challenge
			cmd := m.showLogin()
			return m, cmd
		}
		if msg.needLogin {
			cmd := m.showLogin()
			return m, cmd
		}
		cmd := m.activate(msg)
		return m, cmd
	case chatResult:
		if msg.generation != m.generation || m.chat == nil {
			return m, nil
		}
		if _, quit := msg.msg.(tea.QuitMsg); quit {
			return m, tea.Quit
		}
		cmd := m.chatUpdate(msg.msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for i := range m.fields {
			m.fields[i].Width = max(1, m.width-8)
		}
		cmd := m.chatUpdate(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+q" {
			m.cancelLogin()
			m.page = connectionQuitting
			return m, tea.Quit
		}
		if msg.String() == "ctrl+o" {
			m.manager()
			return m, nil
		}
		if m.page == connectionChat {
			if msg.String() == "enter" && m.chat.page == chatPage && strings.TrimSpace(m.chat.input.Value()) == "/connections" {
				m.chat.resetComposer()
				m.chat.resizeComposer()
				m.manager()
				return m, nil
			}
			cmd := m.chatUpdate(msg)
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c":
			m.cancelLogin()
			m.page = connectionQuitting
			return m, tea.Quit
		case "esc":
			if m.page != connectionList || m.busy {
				m.manager()
				return m, nil
			}
			if m.chat != nil {
				m.page = connectionChat
				m.chat.live.hidden = false
				return m, tea.Batch(tagChat(m.chat.focusPage(), m.generation), tagChat(m.chat.markLiveRead(), m.generation))
			}
			return m, nil
		}
		if m.width < 42 || m.height < 18 {
			return m, nil
		}
		if m.busy {
			return m, nil
		}
		if m.page == connectionList {
			switch msg.String() {
			case "up":
				m.cursor = max(0, m.cursor-1)
			case "down":
				m.cursor = min(len(m.options.Store.Items), m.cursor+1)
			case "n":
				m.page = connectionAdd
				m.notice = ""
				m.fields = connectionFields(3, m.width)
				m.field = 0
				cmd := m.focusField()
				return m, cmd
			case "enter":
				if m.cursor == len(m.options.Store.Items) {
					m.page = connectionAdd
					m.notice = ""
					m.fields = connectionFields(3, m.width)
					m.field = 0
					cmd := m.focusField()
					return m, cmd
				}
				cmd := m.begin(m.options.Store.Items[m.cursor])
				return m, cmd
			}
			return m, nil
		}
		switch msg.String() {
		case "tab", "down":
			m.field = (m.field + 1) % len(m.fields)
			cmd := m.focusField()
			return m, cmd
		case "shift+tab", "up":
			m.field = (m.field + len(m.fields) - 1) % len(m.fields)
			cmd := m.focusField()
			return m, cmd
		case "enter":
			if m.page == connectionAdd {
				c, err := m.options.Store.Add(m.fields[1].Value(), m.fields[0].Value(), strings.TrimSpace(m.fields[2].Value()), "")
				if err != nil {
					m.notice = cleanText(err.Error())
					return m, nil
				}
				cmd := m.begin(c)
				return m, cmd
			}
			if m.challenge == "" && m.field == 0 {
				m.field = 1
				cmd := m.focusField()
				return m, cmd
			}
			cmd := m.login()
			return m, cmd
		}
	case tea.MouseMsg:
		if m.page == connectionChat {
			cmd := m.chatUpdate(msg)
			return m, cmd
		}
		if m.page == connectionList && !m.busy {
			if msg.Button == tea.MouseButtonWheelUp {
				m.cursor = max(0, m.cursor-1)
			} else if msg.Button == tea.MouseButtonWheelDown {
				m.cursor = min(len(m.options.Store.Items), m.cursor+1)
			}
		}
		return m, nil
	}
	if m.page == connectionChat {
		cmd := m.chatUpdate(event)
		return m, cmd
	}
	if len(m.fields) > 0 && !m.busy {
		var cmd tea.Cmd
		m.fields[m.field], cmd = m.fields[m.field].Update(event)
		return m, cmd
	}
	return m, nil
}

func (m connectionModel) View() string {
	// Bubble Tea renders once more before it handles the quit command.
	if m.page == connectionQuitting {
		return ""
	}
	if m.page == connectionChat && m.chat != nil {
		return m.chat.View()
	}
	if m.width < 42 || m.height < 18 {
		return ansi.Hardwrap("Molr\n终端至少需要 42 列 × 18 行。\nCtrl+Q 退出", max(1, m.width), true)
	}
	width, height := m.width-4, m.height-6
	title, footer := "连接管理", "↑↓ 选择 · Enter 连接 · N 新增 · Esc 返回"
	lines := []string{}
	switch m.page {
	case connectionList:
		if len(m.options.Store.Items) == 0 {
			lines = append(lines, muted.Render("尚未添加 Runtime"), "")
		}
		visible := max(1, (height-4)/3)
		start := max(0, m.cursor-visible+1)
		for i := start; i <= len(m.options.Store.Items) && i < start+visible; i++ {
			prefix := "  "
			if i == m.cursor {
				prefix = "› "
			}
			if i == len(m.options.Store.Items) {
				lines = append(lines, accent.Render(prefix+"新增连接"))
				continue
			}
			c := m.options.Store.Items[i]
			label := cleanText(c.Name)
			status := ""
			if c.ID == m.active.ID {
				status = "当前"
			} else if c.ID == m.options.Store.Last {
				status = "上次使用"
			}
			line := spread(prefix+label, muted.Render(status), width)
			if i == m.cursor {
				line = accent.Bold(true).Render(line)
			}
			lines = append(lines, line, muted.Render(ansi.Truncate("  "+c.Server, width, "…")), "")
		}
	case connectionAdd:
		title = "新增连接"
		footer = "Tab 切换输入 · Enter 保存并连接 · Esc 返回"
		for i, label := range []string{"Runtime 地址", "名称（可选）", "CA 证书路径（可选）"} {
			lines = append(lines, muted.Render(label), m.fields[i].View(), "")
		}
	case connectionLogin:
		title = "登录 · " + cleanText(m.target.Name)
		footer = "Tab 切换输入 · Enter 继续 · Esc 返回连接管理"
		lines = append(lines, muted.Render(ansi.Truncate(m.target.Server, width, "…")), "")
		labels := []string{"用户名", "密码"}
		if m.challenge != "" {
			labels = []string{"验证码"}
		}
		for i, label := range labels {
			lines = append(lines, muted.Render(label), m.fields[i].View(), "")
		}
	}
	if m.busy {
		footer = "Esc 取消 · Ctrl+Q 退出"
	}
	body := strings.Join(lines, "\n")
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > height-2 {
		bodyLines = bodyLines[:height-2]
	}
	for len(bodyLines) < height-2 {
		bodyLines = append(bodyLines, "")
	}
	bodyLines = append(bodyLines, "", warning.Render(ansi.Truncate(cleanText(m.notice), width, "…")))
	pad := lipgloss.NewStyle().Width(m.width).Padding(0, 2)
	header := spread(strong.Render(title), "Molr", width)
	return "\n" + pad.Render(header) + "\n\n" + pad.Render(strings.Join(bodyLines, "\n")) + "\n\n" + pad.Render(muted.Render(ansi.Truncate(footer, width, "…"))) + "\n"
}

func (m connectionModel) shutdown() {
	if m.cancelAttempt != nil {
		m.cancelAttempt()
	}
	if m.cancelChat != nil {
		m.cancelChat()
	}
}

var _ tea.Model = connectionModel{}
