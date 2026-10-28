package runtimeclient

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Connection contains an endpoint and local client settings, never a password.
type Connection struct {
	ID          string
	Name        string
	Server      string
	CAFile      string `json:",omitempty"`
	SessionFile string `json:",omitempty"`
}

// Connections keeps the selected endpoint separate from per-connection sessions.
type Connections struct {
	Items []Connection
	Last  string
	path  string
}

// OpenConnections loads the connection list without contacting any Runtime.
func OpenConnections(path string) (*Connections, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = absolute
	s := &Connections{path: path}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<10 {
		return nil, errors.New("连接文件格式无效")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, s) != nil || len(s.Items) > 100 {
		return nil, errors.New("连接文件格式无效")
	}
	ids, servers := map[string]bool{}, map[string]bool{}
	for i, c := range s.Items {
		if c.ID == "" || len(c.ID) > 64 || strings.ContainsFunc(c.ID, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') }) || ids[c.ID] {
			return nil, errors.New("连接标识无效")
		}
		normalized, err := normalizeServer(c.Server)
		if err != nil {
			return nil, err
		}
		c.Server = normalized
		if invalidConnectionName(c.Name) || servers[c.Server] {
			return nil, errors.New("连接名称或地址无效")
		}
		ids[c.ID], servers[c.Server] = true, true
		s.Items[i] = c
	}
	if !ids[s.Last] {
		s.Last = ""
	}
	return s, nil
}
func normalizeServer(server string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("请输入完整的 http(s)://主机[:端口] 地址")
	}
	u.Path = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}
func invalidConnectionName(name string) bool {
	return strings.TrimSpace(name) == "" || len([]rune(name)) > 80 || strings.ContainsFunc(name, unicode.IsControl)
}

// Add saves or reuses an endpoint. Adding a different endpoint never reuses its session.
func (s *Connections) Add(name, server, ca, session string) (Connection, error) {
	normalized, err := normalizeServer(server)
	if err != nil {
		return Connection{}, err
	}
	if ca != "" {
		ca, err = filepath.Abs(ca)
		if err != nil {
			return Connection{}, err
		}
	}
	if session != "" {
		session, err = filepath.Abs(session)
		if err != nil {
			return Connection{}, err
		}
		for _, old := range s.Items {
			if old.Server != normalized && s.SessionPath(old) == session {
				return Connection{}, errors.New("会话文件已用于另一个连接")
			}
		}
	}
	name = strings.TrimSpace(name)
	named := name != ""
	if name == "" {
		u, _ := url.Parse(normalized)
		name = u.Host
	}
	if invalidConnectionName(name) {
		return Connection{}, errors.New("连接名称应为 1–80 个字符")
	}
	next := append([]Connection(nil), s.Items...)
	c := Connection{ID: NewKey(), Name: name, Server: normalized, CAFile: ca, SessionFile: session}
	found := false
	for i, old := range next {
		if old.Server == normalized {
			c.ID = old.ID
			if !named {
				c.Name = old.Name
			}
			if ca == "" {
				c.CAFile = old.CAFile
			}
			if session == "" {
				c.SessionFile = old.SessionFile
			}
			next[i] = c
			found = true
			break
		}
	}
	if !found {
		if len(next) >= 100 {
			return Connection{}, errors.New("最多保存 100 个连接")
		}
		next = append(next, c)
	}
	if err = s.write(next, s.Last); err != nil {
		return Connection{}, err
	}
	return c, nil
}

// Select records the last successfully opened Runtime.
func (s *Connections) Select(id string) error {
	found := false
	for _, c := range s.Items {
		if c.ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("连接不存在")
	}
	return s.write(s.Items, id)
}

// SessionPath isolates login credentials by connection, including on the same host.
func (s *Connections) SessionPath(c Connection) string {
	if c.SessionFile != "" {
		return c.SessionFile
	}
	return filepath.Join(filepath.Dir(s.path), "sessions", c.ID+".json")
}
func (s *Connections) write(items []Connection, last string) error {
	raw, err := json.MarshalIndent(struct {
		Items []Connection
		Last  string
	}{items, last}, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".connections-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.Items, s.Last = items, last
	return nil
}

// StateDirectory is the private directory containing this connection store.
func (s *Connections) StateDirectory() string { return filepath.Dir(s.path) }

// Update edits local metadata. A changed origin creates a separate connection and never reuses credentials.
func (s *Connections) Update(id, name, server, ca string) (Connection, error) {
	normalized, err := normalizeServer(server)
	if err != nil {
		return Connection{}, err
	}
	for i, c := range s.Items {
		if c.ID != id {
			continue
		}
		if c.Server != normalized {
			return s.Add(name, normalized, ca, "")
		}
		if invalidConnectionName(strings.TrimSpace(name)) {
			return c, errors.New("连接名称应为 1–80 个字符")
		}
		if ca != "" {
			ca, err = filepath.Abs(ca)
			if err != nil {
				return c, err
			}
		}
		c.Name = strings.TrimSpace(name)
		c.CAFile = ca
		items := append([]Connection(nil), s.Items...)
		items[i] = c
		return c, s.write(items, s.Last)
	}
	return Connection{}, errors.New("连接不存在")
}

// Remove forgets a local connection; the caller handles session revocation and local draft retention explicitly.
func (s *Connections) Remove(id string) error {
	var next []Connection
	found := false
	for _, c := range s.Items {
		if c.ID == id {
			found = true
		} else {
			next = append(next, c)
		}
	}
	if !found {
		return errors.New("连接不存在")
	}
	last := s.Last
	if last == id {
		last = ""
	}
	return s.write(next, last)
}
