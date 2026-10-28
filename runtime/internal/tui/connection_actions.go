package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/runtimeclient"
)

type connectionLogoutResult struct {
	attempt    uint64
	connection runtimeclient.Connection
	action     string
	err        error
}

func (m *connectionModel) connectionAction(event tea.Msg) (tea.Cmd, bool) {
	if result, ok := event.(connectionLogoutResult); ok {
		if result.attempt != m.attempt {
			return nil, true
		}
		m.busy = false
		if result.err != nil {
			m.notice = "服务端退出未确认；R 重试，F 仅清除本机登录，Esc 返回"
			m.offlineLogout = true
			return nil, true
		}
		if err := m.forgetSession(result.connection); err != nil {
			m.notice = "服务端已退出，本机登录文件清理失败：" + err.Error()
			m.page = connectionList
			return nil, true
		}
		if result.action == "switch" {
			m.options.ForceLogin = true
			return m.begin(result.connection), true
		}
		m.page = connectionList
		m.notice = "已退出登录，连接和本机草稿保留"
		return nil, true
	}
	key, ok := event.(tea.KeyMsg)
	if !ok {
		return nil, false
	}
	name := strings.ToLower(key.String())
	if name == "ctrl+q" || name == "ctrl+c" {
		return nil, false
	}
	if m.offlineLogout {
		switch name {
		case "f":
			if err := m.forgetSession(m.target); err != nil {
				m.notice = "本机登录清理失败：" + err.Error()
				return nil, true
			}
			m.offlineLogout = false
			m.page = connectionList
			m.notice = "已清除本机登录；服务端会话撤销尚未完成"
			return nil, true
		case "r":
			m.offlineLogout = false
			return m.logoutConnection(m.target, m.connectionActionName), true
		case "esc":
			m.offlineLogout = false
			m.page = connectionList
			return nil, true
		}
		return nil, true
	}
	if m.page == connectionConfirm {
		if m.busy {
			return nil, true
		}
		if name == "esc" || name == "n" {
			m.page = connectionList
			return nil, true
		}
		if name != "y" {
			return nil, true
		}
		c := m.target
		if m.connectionActionName == "delete" || m.connectionActionName == "purge" {
			if err := m.flushChat(); err != nil {
				m.notice = err.Error()
				return nil, true
			}
			if _, err := os.Lstat(m.options.Store.SessionPath(c)); err == nil {
				m.notice = "请先按 L 退出此连接的登录，再删除连接"
				m.page = connectionList
				return nil, true
			}
			if m.chat != nil && m.active.ID == c.ID {
				m.notice = "请先退出此连接的登录，再删除连接"
				m.page = connectionList
				return nil, true
			}
			if err := m.options.Store.Remove(c.ID); err != nil {
				m.notice = err.Error()
				return nil, true
			}
			if err := os.Remove(m.options.Store.SessionPath(c)); err != nil && !errors.Is(err, os.ErrNotExist) {
				m.notice = "连接已删除，本机登录文件清理失败"
			} else {
				m.notice = "连接已删除，本机草稿保留"
			}
			if m.connectionActionName == "purge" {
				for key, s := range m.draftStores {
					if len(key) > len(c.ID) && key[:len(c.ID)+1] == c.ID+"/" {
						s.close()
						delete(m.draftStores, key)
					}
				}
				if err := os.RemoveAll(filepath.Join(m.options.Store.StateDirectory(), "drafts", c.ID)); err != nil {
					m.notice = "连接已删除，草稿清理失败"
				} else {
					m.notice = "连接与本机草稿已删除"
				}
			}
			delete(m.cache, c.ID)
			m.cursor = 0
			m.page = connectionList
			return nil, true
		}
		return m.logoutConnection(c, m.connectionActionName), true
	}
	if m.page == connectionEdit && name == "enter" {
		c, err := m.options.Store.Update(m.target.ID, m.fields[1].Value(), m.fields[0].Value(), m.fields[2].Value())
		if err != nil {
			m.notice = err.Error()
			return nil, true
		}
		if c.ID == m.active.ID && m.chat != nil {
			m.flushChat()
			if m.cancelChat != nil {
				m.cancelChat()
			}
			m.chat = nil
			m.active = runtimeclient.Connection{}
		}
		return m.begin(c), true
	}
	if m.page != connectionList || m.busy || m.cursor >= len(m.options.Store.Items) {
		return nil, false
	}
	c := m.options.Store.Items[m.cursor]
	switch name {
	case "e":
		m.target = c
		m.page = connectionEdit
		m.fields = connectionFields(3, m.width)
		m.fields[0].SetValue(c.Server)
		m.fields[1].SetValue(c.Name)
		m.fields[2].SetValue(c.CAFile)
		m.field = 0
		m.notice = "更改地址将保存为另一连接，旧连接仍保留"
		return m.focusField(), true
	case "d", "x", "l", "a":
		m.target = c
		m.page = connectionConfirm
		m.connectionActionName = map[string]string{"d": "delete", "x": "purge", "l": "logout", "a": "switch"}[name]
		m.notice = map[string]string{"d": "删除连接，本机草稿保留。Y 确认 · N 取消", "x": "删除连接及该连接的全部本机草稿。Y 确认 · N 取消", "l": "撤销当前登录，草稿保留。Y 确认 · N 取消", "a": "退出当前账号并登录其他账号，草稿按账号保留。Y 确认 · N 取消"}[name]
		return nil, true
	}
	return nil, false
}
func (m *connectionModel) flushChat() error {
	if m.chat != nil {
		if err := m.chat.persistDrafts(); err != nil {
			m.notice = "草稿未保存：" + err.Error()
			return err
		}
	}
	return nil
}
func (m *connectionModel) forgetSession(c runtimeclient.Connection) error {
	if err := m.flushChat(); err != nil {
		return err
	}
	if m.active.ID == c.ID {
		if m.cancelChat != nil {
			m.cancelChat()
		}
		m.chat = nil
		m.active = runtimeclient.Connection{}
		m.generation++
	}
	delete(m.cache, c.ID)
	if err := os.Remove(m.options.Store.SessionPath(c)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func (m *connectionModel) logoutConnection(c runtimeclient.Connection, action string) tea.Cmd {
	if err := m.flushChat(); err != nil {
		return nil
	}
	m.busy = true
	m.connectionActionName = action
	m.attempt++
	attempt := m.attempt
	ctx := m.ctx
	sessionPath := m.options.Store.SessionPath(c)
	return func() tea.Msg {
		out := connectionLogoutResult{attempt: attempt, connection: c, action: action}
		client, err := runtimeclient.New(c.Server, c.CAFile)
		if err != nil {
			out.err = err
			return out
		}
		restored, err := client.Restore(sessionPath)
		if err != nil {
			out.err = err
			return out
		}
		if restored {
			err = client.Do(ctx, "POST", "/api/auth/logout", struct{}{}, nil)
			var e *runtimeclient.Error
			if errors.As(err, &e) && e.Status == 401 {
				err = nil
			}
		}
		out.err = err
		return out
	}
}
