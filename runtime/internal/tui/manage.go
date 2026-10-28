package tui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	keybind "github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
)

type manageRow struct {
	label string
	open  func(*model) tea.Cmd
}
type formField struct {
	key, label string
	input      textinput.Model
	choices    []string
	labels     []string
}
type manageScreen struct {
	title, body, notice   string
	rows                  []manageRow
	cursor, scroll, field int
	fields                []formField
	submit                func(map[string]string) (func(context.Context, *runtimeclient.Client) error, error)
	localSubmit           func(*model, map[string]string) tea.Cmd
	submitLabel           string
	reload                func(context.Context, *runtimeclient.Client) (manageScreen, error)
	returnTo              string
	afterSuccess          func(*model) tea.Cmd
	cancellable           bool
	dirty, confirmLeave   bool
	poll, followSelection bool
}
type manageState struct {
	screen manageScreen
	stack  []manageScreen
	serial uint64
	busy   bool
	cancel context.CancelFunc
}
type managedPoll struct{ serial uint64 }
type managedResult struct {
	serial   uint64
	screen   manageScreen
	err      error
	mutation bool
	returnTo string
}

func field(key, label, value string, choices ...string) formField {
	input := textinput.New()
	input.KeyMap.Paste = keybind.NewBinding(keybind.WithDisabled())
	input.Prompt = "› "
	input.CharLimit = 8192
	input.SetValue(value)
	return formField{key: key, label: label, input: input, choices: choices}
}
func values(s manageScreen) map[string]string {
	v := map[string]string{}
	for _, f := range s.fields {
		v[f.key] = f.input.Value()
	}
	return v
}
func (m *model) showScreen(s manageScreen) tea.Cmd {
	if m.page != managePage {
		m.openPage(managePage)
		m.manage.stack = nil
	} else {
		m.manage.stack = append(m.manage.stack, m.manage.screen)
	}
	m.manage.serial++
	m.manage.busy = false
	m.manage.screen = s
	if len(s.fields) > 0 {
		return m.manage.screen.fields[0].input.Focus()
	}
	return nil
}
func (m *model) loadScreen(title string, load func(context.Context, *runtimeclient.Client) (manageScreen, error)) tea.Cmd {
	if m.live == nil {
		m.notice = "此操作需要连接 Runtime"
		return nil
	}
	m.showScreen(manageScreen{title: title, reload: load})
	return m.reloadScreen()
}
func (m *model) reloadScreen() tea.Cmd {
	s := m.manage.screen
	if m.manage.busy || s.reload == nil {
		return nil
	}
	m.manage.serial++
	serial := m.manage.serial
	m.manage.busy = true
	ctx, cancel := context.WithCancel(m.live.ctx)
	m.manage.cancel = cancel
	client := m.live.client
	return func() tea.Msg {
		defer cancel()
		v, err := s.reload(ctx, client)
		if v.reload == nil {
			v.reload = s.reload
		}
		v.cursor = s.cursor
		v.scroll = s.scroll
		return managedResult{serial: serial, screen: v, err: err}
	}
}
func (m *model) closeScreen() tea.Cmd {
	m.manage.serial++
	m.manage.busy = false
	if n := len(m.manage.stack); n > 0 {
		m.manage.screen = m.manage.stack[n-1]
		m.manage.stack = m.manage.stack[:n-1]
		return nil
	}
	return m.goBack()
}
func (m *model) confirmAction(title, detail, label string, action func(context.Context, *runtimeclient.Client) error, returnTo string) tea.Cmd {
	return m.showScreen(manageScreen{title: title, body: detail, submitLabel: label, returnTo: returnTo, submit: func(map[string]string) (func(context.Context, *runtimeclient.Client) error, error) {
		return action, nil
	}})
}
func (m *model) updateManaged(event tea.Msg) (tea.Cmd, bool) {
	if result, ok := event.(managedResult); ok {
		if m.page != managePage || result.serial != m.manage.serial {
			return nil, true
		}
		m.manage.busy = false
		if result.err != nil {
			m.manage.screen.notice = cleanText(result.err.Error())
			return nil, true
		}
		if result.mutation {
			if done := m.manage.screen.afterSuccess; done != nil {
				return done(m), true
			}
			if result.returnTo != "" {
				m.manage.stack = nil
				m.page = chatPage
				m.back = nil
				return tea.Batch(m.openManagement(result.returnTo, ""), m.scheduleLive()), true
			}
			m.closeScreen()
			if m.page == managePage {
				m.manage.screen.notice = "操作完成"
				return tea.Batch(m.reloadScreen(), m.scheduleLive()), true
			}
			return m.scheduleLive(), true
		}
		result.screen.cursor = min(result.screen.cursor, max(0, len(result.screen.rows)-1))
		m.manage.screen = result.screen
		if len(result.screen.fields) > 0 {
			return m.manage.screen.fields[0].input.Focus(), true
		}
		if result.screen.poll {
			serial := m.manage.serial
			return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return managedPoll{serial} }), true
		}
		return nil, true
	}
	if tick, ok := event.(managedPoll); ok {
		if m.page == managePage && tick.serial == m.manage.serial && m.manage.screen.poll {
			return m.reloadScreen(), true
		}
		return nil, true
	}
	if m.page != managePage {
		return nil, false
	}
	if _, ok := event.(tea.WindowSizeMsg); ok {
		return nil, false
	}
	key, ok := event.(tea.KeyMsg)
	if !ok {
		return nil, false
	}
	s := &m.manage.screen
	if key.String() == "ctrl+q" {
		return tea.Quit, true
	}
	if m.manage.busy {
		if key.String() == "esc" && m.manage.cancel != nil {
			if s.cancellable {
				m.manage.cancel()
			} else if s.submit == nil {
				m.manage.cancel()
				return m.closeScreen(), true
			}
		}
		return nil, true
	}
	if s.confirmLeave {
		switch key.String() {
		case "y", "Y":
			return m.closeScreen(), true
		case "n", "N", "esc":
			s.confirmLeave = false
			s.notice = ""
		}
		return nil, true
	}
	if key.String() == "esc" {
		if s.dirty {
			s.confirmLeave = true
			s.notice = "放弃未保存修改？Y 放弃 · N 继续编辑"
			return nil, true
		}
		return m.closeScreen(), true
	}
	if key.String() == "ctrl+r" {
		if s.dirty {
			s.notice = "有未保存修改，请先保存或返回"
			return nil, true
		}
		return m.reloadScreen(), true
	}
	switch key.String() {
	case "pgup":
		s.followSelection = false
		s.scroll = max(0, s.scroll-max(1, m.bodyHeight()/2))
		return nil, true
	case "pgdown":
		s.followSelection = false
		s.scroll += max(1, m.bodyHeight()/2)
		return nil, true
	}
	if len(s.fields) > 0 {
		switch key.String() {
		case "tab", "down":
			s.fields[s.field].input.Blur()
			s.field = (s.field + 1) % len(s.fields)
			return s.fields[s.field].input.Focus(), true
		case "shift+tab", "up":
			s.fields[s.field].input.Blur()
			s.field = (s.field + len(s.fields) - 1) % len(s.fields)
			return s.fields[s.field].input.Focus(), true
		case "left", "right":
			f := &s.fields[s.field]
			if len(f.choices) > 0 {
				i := 0
				for n, v := range f.choices {
					if v == f.input.Value() {
						i = n
					}
				}
				if key.String() == "left" {
					i = (i + len(f.choices) - 1) % len(f.choices)
				} else {
					i = (i + 1) % len(f.choices)
				}
				f.input.SetValue(f.choices[i])
				s.dirty = true
				return nil, true
			}
		}
	}
	if key.String() == "enter" {
		if s.localSubmit != nil {
			return s.localSubmit(m, values(*s)), true
		}
		if s.submit != nil {
			action, err := s.submit(values(*s))
			if err != nil {
				s.notice = cleanText(err.Error())
				return nil, true
			}
			m.manage.serial++
			serial := m.manage.serial
			m.manage.busy = true
			s.notice = "正在提交…"
			ctx, cancel := context.WithCancel(m.live.ctx)
			m.manage.cancel = cancel
			client, returnTo := m.live.client, s.returnTo
			return func() tea.Msg {
				defer cancel()
				return managedResult{serial: serial, err: action(ctx, client), mutation: true, returnTo: returnTo}
			}, true
		}
		if len(s.rows) > 0 {
			s.cursor = min(s.cursor, len(s.rows)-1)
			return s.rows[s.cursor].open(m), true
		}
	}
	if len(s.fields) > 0 {
		f := &s.fields[s.field]
		if len(f.choices) > 0 {
			return nil, true
		}
		before := f.input.Value()
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(key)
		s.dirty = s.dirty || before != f.input.Value()
		return cmd, true
	}
	switch key.String() {
	case "up":
		s.followSelection = true
		s.cursor = max(0, s.cursor-1)
	case "down":
		s.followSelection = true
		s.cursor = min(max(0, len(s.rows)-1), s.cursor+1)
	}
	return nil, true
}
func (m model) managedView() string {
	s := m.manage.screen
	width := m.contentWidth()
	height := max(1, m.bodyHeight()-3)
	var lines []string
	if s.body != "" {
		lines = append(lines, strings.Split(ansi.Hardwrap(cleanText(s.body), width, true), "\n")...)
		lines = append(lines, "")
	}
	if len(s.fields) > 0 {
		for i, f := range s.fields {
			f.input.Width = max(1, width-6)
			value := f.input.View()
			if len(f.choices) > 0 {
				value = f.input.Value()
				for j, v := range f.choices {
					if v == value && j < len(f.labels) {
						value = f.labels[j]
						break
					}
				}
				value = "‹ " + cleanText(value) + " ›"
			}
			prefix := "  "
			if i == s.field {
				prefix = "› "
			}
			lines = append(lines, cellTruncate(prefix+f.label, width, "…"), cellTruncate("  "+value, width, "…"))
		}
		// Keep the active field visible; body and field descriptions share one scroll area.
		target := len(lines) - 2*(len(s.fields)-s.field)
		if target < s.scroll {
			s.scroll = target
		}
		if target+2 > s.scroll+height {
			s.scroll = target + 2 - height
		}
	} else {
		for i, r := range s.rows {
			prefix := "  "
			if i == s.cursor {
				prefix = "› "
			}
			lines = append(lines, cellTruncate(prefix+strings.ReplaceAll(cleanText(r.label), "\n", " "), width, "…"))
		}
		if len(s.rows) > 0 && s.followSelection {
			target := len(lines) - len(s.rows) + s.cursor
			if target < s.scroll {
				s.scroll = target
			}
			if target >= s.scroll+height {
				s.scroll = target - height + 1
			}
		}
	}
	s.scroll = max(0, min(s.scroll, max(0, len(lines)-height)))
	lines = lines[s.scroll:min(len(lines), s.scroll+height)]
	for len(lines) < height {
		lines = append(lines, "")
	}
	notice := s.notice
	if m.manage.busy {
		notice = "正在处理…"
	}
	if notice == "" && (s.submit != nil || s.localSubmit != nil) {
		notice = "Enter " + s.submitLabel
	}
	lines = append(lines, "", cellTruncate(cleanText(notice), width, "…"))
	return strings.Join(lines, "\n")
}
func getAll[T any](ctx context.Context, c *runtimeclient.Client, path string) ([]T, error) {
	var items []T
	cursor := ""
	seen := map[string]bool{}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for {
		var page core.Page[T]
		if err := c.Do(ctx, "GET", path+sep+"limit=200&cursor="+url.QueryEscape(cursor), nil, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			return items, nil
		}
		if seen[page.NextCursor] {
			return nil, fmt.Errorf("分页游标没有前进")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}
func mutation(version core.Version) core.Mutation {
	return core.Mutation{IdempotencyKey: runtimeclient.NewKey(), ExpectedVersion: version}
}
func (m *model) assistantID() core.ID {
	if len(m.assistants) == 0 {
		return ""
	}
	return m.assistants[m.selected].id
}
