package runtimeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionsRememberLastAndIsolateSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	s, err := OpenConnections(path)
	if err != nil || s.Last != "" || len(s.Items) != 0 {
		t.Fatal(s, err)
	}
	a, err := s.Add("本机", "http://LOCALHOST:8080/", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Server != "http://localhost:8080" {
		t.Fatal(a.Server)
	}
	if err = s.Select(a.ID); err != nil {
		t.Fatal(err)
	}
	b, err := s.Add("另一台", "https://example.invalid", "/tmp/ca.pem", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionPath(a) == s.SessionPath(b) || s.Last != a.ID {
		t.Fatal("adding connection changed active session")
	}
	a2, err := s.Add("重命名", "http://localhost:8080", "", "")
	if err != nil || a.ID != a2.ID || len(s.Items) != 2 {
		t.Fatal("duplicate endpoint")
	}
	same, err := s.Add("", a.Server, "", "")
	if err != nil || same.Name != "重命名" {
		t.Fatal("opening existing address overwrote its name")
	}
	if err = s.Select(b.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := OpenConnections(path)
	if err != nil || loaded.Last != b.ID || len(loaded.Items) != 2 || loaded.Items[1].CAFile != "/tmp/ca.pem" {
		t.Fatal(loaded, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config permissions")
	}
	for _, bad := range []string{"file:///etc/passwd", "https://user:secret@example.com", "https://example.com/path", "https://example.com?token=secret"} {
		if _, err = s.Add("bad", bad, "", ""); err == nil {
			t.Fatal(bad)
		}
	}
	if err = s.Select("missing"); err == nil || s.Last != b.ID {
		t.Fatal("invalid last connection")
	}
	override, err := s.Add("独立登录", "https://session.example.invalid", "", filepath.Join(t.TempDir(), "session.json"))
	if _, reuseErr := s.Add("bad reuse", "https://another.invalid", "", override.SessionFile); reuseErr == nil {
		t.Fatal("session file shared across endpoints")
	}
	if err != nil || s.SessionPath(override) != override.SessionFile {
		t.Fatal("session override")
	}
}

func TestConnectionsRejectInvalidStoreAndPreserveOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	os.WriteFile(path, []byte(`{"Items":[{"ID":"../escape","Name":"bad","Server":"https://example.com"}]}`), 0600)
	if _, err := OpenConnections(path); err == nil {
		t.Fatal("unsafe connection ID")
	}
	os.WriteFile(path, []byte(`{"Items":[],"Last":"deleted"}`), 0600)
	s, err := OpenConnections(path)
	if err != nil || s.Last != "" {
		t.Fatal("stale last selection")
	}
	os.Remove(path)
	os.Mkdir(path, 0700)
	if _, err = s.Add("test", "https://example.com", "", ""); err == nil || len(s.Items) != 0 {
		t.Fatal("failed write changed state")
	}
}
