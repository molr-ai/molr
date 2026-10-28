package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
)

func connectionFixture(t *testing.T, owner string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/notifications":
			fmt.Fprint(w, `{"Items":[]}`)
			return
		case "/api/auth/csrf":
			fmt.Fprint(w, `{"csrf_token":"pre"}`)
		case "/api/auth/login":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "secret" {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"code":"unauthenticated"}`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: owner, Path: "/"})
			fmt.Fprint(w, `{"csrf_token":"proof"}`)
		case "/api/auth/me":
			c, err := r.Cookie("session")
			if err != nil || c.Value != owner {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"code":"unauthenticated"}`)
				return
			}
			fmt.Fprintf(w, `{"OwnerID":%q}`, owner)
		case "/api/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: ready\ndata: {}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(s.Close)
	return s
}
func keyConnection(m connectionModel, key tea.KeyType) connectionModel {
	next, _ := m.Update(tea.KeyMsg{Type: key})
	return next.(connectionModel)
}
func applyConnectionCommand(m connectionModel, cmd tea.Cmd) connectionModel {
	next, _ := m.Update(cmd())
	return next.(connectionModel)
}

func TestConnectionManagerQuitDuringForms(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlQ} {
		for _, page := range []string{"list", "add", "login", "mfa", "pending-login"} {
			t.Run(tea.KeyMsg{Type: key}.String()+"/"+page, func(t *testing.T) {
				store, err := runtimeclient.OpenConnections(filepath.Join(t.TempDir(), "connections.json"))
				if err != nil {
					t.Fatal(err)
				}
				m := newConnectionModel(t.Context(), ConnectionOptions{Store: store}, nil)
				defer func() { m.shutdown() }()
				switch page {
				case "add":
					m = keyConnection(m, tea.KeyEnter)
				case "login", "pending-login":
					m.showLogin()
				case "mfa":
					m.challenge = "challenge"
					m.showLogin()
				}
				m.busy = page == "pending-login"
				m.attemptContext, m.cancelAttempt = context.WithCancel(t.Context())
				attempt := m.attempt
				for i := range m.fields {
					m.fields[i].SetValue("private-input")
				}
				next, cmd := m.Update(tea.KeyMsg{Type: key})
				m = next.(connectionModel)
				if cmd == nil {
					t.Fatal("exit did not return a command")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("exit did not request quit")
				}
				if m.attemptContext.Err() != context.Canceled || len(m.fields) != 0 || m.busy {
					t.Fatal("exit retained pending login or form data")
				}
				// Bubble Tea renders after Update, before processing the quit command.
				if view := m.View(); view != "" {
					t.Fatalf("exit rendered a page: %q", view)
				}
				for _, event := range []tea.Msg{
					connectionResult{attempt: attempt, needLogin: true},
					tea.KeyMsg{Type: tea.KeyTab},
					tea.KeyMsg{Type: tea.KeyCtrlO},
				} {
					next, cmd = m.Update(event)
					m = next.(connectionModel)
					if cmd != nil || m.View() != "" || len(m.fields) != 0 {
						t.Fatal("queued event reopened a page during exit")
					}
				}
			})
		}
	}
}

func TestConnectionManagerFirstRunLoginAndLastRestore(t *testing.T) {
	s := connectionFixture(t, "account-a")
	store, _ := runtimeclient.OpenConnections(filepath.Join(t.TempDir(), "connections.json"))
	m := newConnectionModel(t.Context(), ConnectionOptions{Store: store}, nil)
	t.Cleanup(func() { m.shutdown() })
	if m.Init() != nil || !strings.Contains(m.View(), "尚未添加 Runtime") {
		t.Fatal("first run did not open manager")
	}
	m = keyConnection(m, tea.KeyEnter)
	if m.page != connectionAdd {
		t.Fatal("add page")
	}
	m.fields[0].SetValue(s.URL)
	m.fields[1].SetValue("测试连接")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(connectionModel)
	if !m.busy || m.page != connectionList {
		t.Fatal("connect not started")
	}
	m = applyConnectionCommand(m, cmd)
	if m.page != connectionLogin || store.Last != "" {
		t.Fatal("login or premature last connection")
	}
	m.fields[0].SetValue("tester")
	m.fields[1].SetValue("secret")
	m.field = 1
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(connectionModel)
	if strings.Contains(m.View(), "secret") || m.fields[1].Value() != "" {
		t.Fatal("password exposed")
	}
	m = applyConnectionCommand(m, cmd)
	if m.page != connectionChat || store.Last != m.active.ID || m.owner != "account-a" {
		t.Fatal("login did not activate")
	}
	m = keyConnection(m, tea.KeyCtrlO)
	if m.page != connectionList || !m.chat.live.hidden {
		t.Fatal("switch entry did not hide chat")
	}
	m = keyConnection(m, tea.KeyEsc)
	if m.page != connectionChat || m.chat.live.hidden {
		t.Fatal("cancel did not restore chat")
	}
	loaded, _ := runtimeclient.OpenConnections(filepath.Join(filepath.Dir(store.SessionPath(m.active)), "..", "connections.json"))
	restored := newConnectionModel(t.Context(), ConnectionOptions{Store: loaded, Initial: loaded.Last}, nil)
	defer func() { restored.shutdown() }()
	next, cmd = restored.Update(restored.Init()())
	restored = next.(connectionModel)
	restored = applyConnectionCommand(restored, cmd)
	if restored.page != connectionList || !strings.Contains(restored.notice, "另一个 TUI") {
		t.Fatal("second process should not open the same account drafts")
	}
	m.shutdown()
	cmd = restored.begin(loaded.Items[0])
	restored = applyConnectionCommand(restored, cmd)
	if restored.page != connectionChat || restored.active.Server != s.URL {
		t.Fatal("last connection/session not restored")
	}
	for _, size := range [][2]int{{42, 18}, {80, 24}, {120, 32}} {
		next, _ := restored.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		restored = next.(connectionModel)
		if len(strings.Split(restored.View(), "\n")) != size[1] {
			t.Fatal("chat no longer fills terminal")
		}
		restored = keyConnection(restored, tea.KeyCtrlO)
		for _, line := range strings.Split(ansi.Strip(restored.View()), "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatal("connection page overflow")
			}
		}
		restored = keyConnection(restored, tea.KeyEsc)
	}
}

func TestConnectionSwitchCancelsOldEventsAndIsolatesAccounts(t *testing.T) {
	aServer, bServer := connectionFixture(t, "account-a"), connectionFixture(t, "account-b")
	store, _ := runtimeclient.OpenConnections(filepath.Join(t.TempDir(), "connections.json"))
	a, _ := store.Add("A", aServer.URL, "", "")
	b, _ := store.Add("B", bServer.URL, "", "")
	m := newConnectionModel(t.Context(), ConnectionOptions{Store: store}, nil)
	defer func() { m.shutdown() }()
	connect := func(c runtimeclient.Connection, owner core.ID) {
		client, _ := runtimeclient.New(c.Server, "")
		_, err := client.Login(t.Context(), "tester", "secret")
		if err != nil {
			t.Fatal(err)
		}
		m.manager()
		m.target = c
		m.activate(connectionResult{client: client, owner: owner, signed: true})
	}
	connect(a, "account-a")
	oldContext := m.chat.live.ctx
	oldGeneration := m.generation
	m.chat.input.SetValue("A 草稿")
	connect(b, "account-b")
	if oldContext.Err() != context.Canceled || m.chat.input.Value() != "" {
		t.Fatal("switch retained old connection state")
	}
	next, _ := m.Update(chatResult{oldGeneration, snapshotMsg{err: fmt.Errorf("old response")}})
	m = next.(connectionModel)
	if strings.Contains(m.chat.notice, "old response") {
		t.Fatal("stale result crossed connections")
	}
	connect(a, "account-a")
	if m.chat.input.Value() != "A 草稿" {
		t.Fatal("return lost draft")
	}
	connect(b, "account-b")
	connect(a, "another-account")
	if m.chat.input.Value() != "" {
		t.Fatal("draft leaked between accounts")
	}
	// A cancelled login attempt must not reopen its page when its HTTP call returns.
	cmd := m.begin(b)
	attempt := m.attempt
	m.manager()
	_ = cmd
	next, _ = m.Update(connectionResult{attempt: attempt, needLogin: true})
	m = next.(connectionModel)
	if m.page != connectionList || m.busy {
		t.Fatal("cancelled attempt changed page")
	}
}

func TestConnectionManagerNonTerminalAndNestedCommands(t *testing.T) {
	store, _ := runtimeclient.OpenConnections(filepath.Join(t.TempDir(), "connections.json"))
	if err := RunManaged(t.Context(), strings.NewReader(""), &strings.Builder{}, ConnectionOptions{Store: store}); err == nil {
		t.Fatal("non-terminal accepted")
	}
	wrapped := tagChat(tea.Batch(func() tea.Msg { return "one" }, func() tea.Msg { return "two" }), 9)
	batch := wrapped().(tea.BatchMsg)
	if len(batch) != 2 || batch[0]().(chatResult).generation != 9 || batch[1]().(chatResult).msg != "two" {
		t.Fatal("nested commands lost connection identity")
	}
}

func TestConnectionFailureAndMFAStayInManagerFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/notifications":
			fmt.Fprint(w, `{"Items":[]}`)
			return
		case "/api/auth/csrf":
			fmt.Fprint(w, `{"csrf_token":"pre"}`)
		case "/api/auth/login":
			fmt.Fprint(w, `{"challenge":"challenge"}`)
		case "/api/auth/mfa/complete":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != "123456" {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"code":"unauthenticated"}`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "mfa", Path: "/"})
			fmt.Fprint(w, `{"csrf_token":"proof"}`)
		case "/api/auth/me":
			fmt.Fprint(w, `{"OwnerID":"mfa-owner"}`)
		case "/api/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	store, _ := runtimeclient.OpenConnections(filepath.Join(t.TempDir(), "connections.json"))
	first, _ := store.Add("first", "https://first.invalid", "", "")
	target, _ := store.Add("MFA", server.URL, "", "")
	store.Select(first.ID)
	m := newConnectionModel(t.Context(), ConnectionOptions{Store: store}, nil)
	defer func() { m.shutdown() }()
	cmd := m.begin(target)
	m = applyConnectionCommand(m, cmd)
	m.fields[0].SetValue("tester")
	m.fields[1].SetValue("secret")
	m.field = 1
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(connectionModel)
	m = applyConnectionCommand(m, cmd)
	if m.challenge != "challenge" || len(m.fields) != 1 || !strings.Contains(m.View(), "验证码") {
		t.Fatal("MFA page missing")
	}
	m.fields[0].SetValue("wrong")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(connectionModel)
	m = applyConnectionCommand(m, cmd)
	if m.page != connectionLogin || store.Last != first.ID || m.busy {
		t.Fatal("failed MFA replaced last connection")
	}
	m.fields[0].SetValue("123456")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(connectionModel)
	m = applyConnectionCommand(m, cmd)
	if m.owner != "mfa-owner" || store.Last != target.ID {
		t.Fatal("MFA did not activate connection")
	}
	m.manager()
	m.target = first
	m.busy = true
	next, _ = m.Update(connectionResult{attempt: m.attempt, err: fmt.Errorf("无法连接 Runtime")})
	m = next.(connectionModel)
	if m.page != connectionList || m.busy || !strings.Contains(m.View(), "无法连接 Runtime") || store.Last != target.ID {
		t.Fatal("connection failure lost manager or last selection")
	}
}
