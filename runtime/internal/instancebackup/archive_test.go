package instancebackup_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/storage/sqlite"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"molr.ai/runtime/internal/app"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/installrecord"
	backup "molr.ai/runtime/internal/instancebackup"
	"molr.ai/runtime/internal/persistence"
	"time"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func put(t *testing.T, p, body string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(p), 0700))
	must(t, os.WriteFile(p, []byte(body), 0600))
}
func source(t *testing.T, mode string) (installrecord.Record, config.Config) {
	t.Helper()
	root := t.TempDir()
	r := installrecord.Record{Mode: mode, DataDir: filepath.Join(root, "data"), Paths: installrecord.Paths{Config: filepath.Join(root, "config"), Program: filepath.Join(root, "program")}, Build: installrecord.Build{Version: "0.1.0", Commit: strings.Repeat("a", 40), OS: runtime.GOOS, Arch: runtime.GOARCH}}
	cfg := config.Defaults(r.DataDir)
	cfg.HTTP.TLSMode = config.LocalHTTP
	cfg.HTTP.Listen = "127.0.0.1:8787"
	cfg.HTTP.PublicURL = "http://127.0.0.1:8787"
	cfg.Sandbox.Enabled = false
	cfg.Sandbox.WorkRoot = filepath.Join(root, "work")
	cfg.Sandbox.Directories = []config.SandboxDirectory{{Path: filepath.Join(root, "external")}}
	must(t, os.MkdirAll(cfg.Sandbox.WorkRoot, 0700))
	put(t, filepath.Join(root, "external", "excluded.txt"), "outside")
	raw, _ := json.Marshal(cfg)
	put(t, filepath.Join(r.Paths.Config, "server.json"), string(raw))
	for _, p := range []string{"bin/molr", "bin/molr-setup", "LICENSE", "THIRD_PARTY_NOTICES.md"} {
		put(t, filepath.Join(r.Paths.Program, p), "test-program")
	}
	raw, _ = json.Marshal(r.Build)
	put(t, filepath.Join(r.Paths.Program, "release.json"), string(raw))
	must(t, os.MkdirAll(r.DataDir, 0700))
	return r, cfg
}
func TestRuntimeArchiveRoundTrip(t *testing.T) {
	r, cfg := source(t, "server")
	ctx := t.Context()
	bundle, err := app.Build(ctx, cfg)
	must(t, err)
	password := "synthetic-backup-password-123!"
	account, err := bundle.Bootstrap.Initialize(ctx, auth.InitializeRequest{Username: "owner", Password: core.NewSecret(password)})
	must(t, err)
	login, err := bundle.Services.Auth.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret(password)})
	must(t, err)
	token := login.Session.Token
	must(t, bundle.Database.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := assistant.NewRepository(tx.Store()).Insert(ctx, assistant.Assistant{ID: "assistant-backup", OwnerID: account.ID, Name: "Backup assistant", State: assistant.Active, ConversationID: "chat-backup", Version: 1}); err != nil {
			return err
		}
		if err := conversation.NewRepository(tx.Store()).Insert(ctx, conversation.Conversation{ID: "chat-backup", OwnerID: account.ID, PeerID: "assistant-backup", Version: 1}); err != nil {
			return err
		}
		_, err := conversation.NewRepository(tx.Store()).Append(ctx, account.ID, conversation.Message{ID: "message-backup", ConversationID: "chat-backup", Sender: conversation.User, SenderID: account.ID, Content: conversation.Content{Kind: conversation.Text, Text: "Preserve this conversation"}, CreatedAt: time.Now()})
		return err
	}))
	must(t, bundle.Close())
	put(t, filepath.Join(cfg.Sandbox.WorkRoot, "assistant", "project.txt"), "project content")
	put(t, filepath.Join(r.DataDir, "files", "attachment.txt"), "attachment content")
	put(t, filepath.Join(r.DataDir, "users", "memory.txt"), "memory content")
	put(t, filepath.Join(r.DataDir, "backups", "old.txt"), "not recursive")
	put(t, filepath.Join(r.DataDir, "sandbox", "runtime", "generated", "busybox"), "native cache")
	must(t, os.Symlink("busybox", filepath.Join(r.DataDir, "sandbox", "runtime", "generated", "sh")))
	stage := t.TempDir()
	m, err := backup.Capture(ctx, stage, r)
	must(t, err)
	if len(m.Excluded) != 1 {
		t.Fatal("external directory reference lost")
	}
	for _, e := range m.Files {
		if strings.Contains(e.Path, "excluded.txt") || strings.Contains(e.Path, "old.txt") || strings.Contains(e.Path, "sandbox/runtime") {
			t.Fatal("external/history content included")
		}
	}
	archive := filepath.Join(t.TempDir(), "instance.tar.gz")
	must(t, backup.Pack(ctx, stage, archive, m))
	if backup.Pack(ctx, stage, archive, m) == nil {
		t.Fatal("overwrote archive")
	}
	extracted, manifest, err := backup.Unpack(ctx, archive, t.TempDir())
	must(t, err)
	must(t, backup.CheckDatabase(ctx, extracted, manifest))
	for p, want := range map[string]string{"work/assistant/project.txt": "project content", "data/files/attachment.txt": "attachment content", "data/users/memory.txt": "memory content"} {
		b, err := os.ReadFile(filepath.Join(extracted, p))
		must(t, err)
		if string(b) != want {
			t.Fatalf("%s lost", p)
		}
	}
	target := filepath.Join(extracted, "data")
	mapped, _, err := backup.Configuration(manifest, target, filepath.Join(extracted, "config"), filepath.Join(extracted, "work"))
	must(t, err)
	raw, _ := json.Marshal(mapped)
	restoredConfig, err := config.Load(bytes.NewReader(raw))
	must(t, err)
	restored, err := app.Build(ctx, restoredConfig)
	must(t, err)
	defer restored.Close()
	_, err = restored.Services.Auth.Authenticate(ctx, auth.Authentication{Session: token})
	if core.CodeOf(err) != core.Unauthenticated {
		t.Fatal("old session remains usable", err)
	}
	result, err := restored.Services.Auth.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret(password)})
	must(t, err)
	p, err := restored.Services.Auth.Authenticate(ctx, auth.Authentication{Session: result.Session.Token})
	must(t, err)
	if p.OwnerID != account.ID {
		t.Fatal("owner changed")
	}
	a, err := restored.Services.Assistants.Get(ctx, p, "assistant-backup")
	must(t, err)
	if a.Name != "Backup assistant" {
		t.Fatal("assistant changed")
	}
	messages, err := restored.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: "chat-backup"})
	must(t, err)
	if len(messages.Items) != 1 || messages.Items[0].Content.Text != "Preserve this conversation" {
		t.Fatal("chat history lost")
	}
	oldKey, err := os.ReadFile(filepath.Join(r.DataDir, "keys", "master.key"))
	must(t, err)
	key, err := os.ReadFile(filepath.Join(target, "keys", "master.key"))
	must(t, err)
	if !bytes.Equal(oldKey, key) {
		t.Fatal("master key changed")
	}
}
func TestSandboxArchiveKeepsStateWithoutUserFiles(t *testing.T) {
	r, _ := source(t, "sandbox")
	db, err := sqlite.Open(filepath.Join(r.DataDir, "sandbox.db"))
	must(t, err)
	must(t, db.Migrate(t.Context()))
	must(t, db.Close())
	put(t, filepath.Join(r.DataDir, "sandbox-token"), "synthetic-node-identity")
	put(t, filepath.Join(r.DataDir, "shell-output", "log"), "not configuration")
	stage := t.TempDir()
	m, err := backup.Capture(t.Context(), stage, r)
	must(t, err)
	for _, e := range m.Files {
		if strings.HasPrefix(e.Path, "work") || strings.Contains(e.Path, "shell-output") {
			t.Fatal("sandbox user data included")
		}
	}
	must(t, backup.CheckDatabase(t.Context(), stage, m))
	b, err := os.ReadFile(filepath.Join(stage, "data", "sandbox-token"))
	must(t, err)
	if string(b) != "synthetic-node-identity" {
		t.Fatal("identity lost")
	}
}
func TestCrossPlatformConfiguration(t *testing.T) {
	r, cfg := source(t, "server")
	raw, _ := json.Marshal(cfg)
	var c map[string]any
	json.Unmarshal(raw, &c)
	m := &backup.Manifest{Mode: "server", Build: r.Build, Config: c}
	m.Build.OS = "windows"
	sourceSandbox := c["sandbox"].(map[string]any)
	sourceSandbox["network_presets"] = []any{"network"}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		m.Build.OS = "linux"
		sourceSandbox["network_presets"] = []any{"internet", "lan", "host"}
	}
	result, notes, err := backup.Configuration(m, filepath.Join(t.TempDir(), "data"), t.TempDir(), "")
	must(t, err)
	if len(notes) == 0 {
		t.Fatal("platform change not reported")
	}
	s := result["sandbox"].(map[string]any)
	if s["enabled"] != false {
		t.Fatal("changed execution permission")
	}
}
func TestArchivePreservesLinksAndRejectsTampering(t *testing.T) {
	stage := t.TempDir()
	put(t, filepath.Join(stage, "work", "file"), "value")
	must(t, os.MkdirAll(filepath.Join(stage, "work", "empty"), 0700))
	if err := os.Symlink("file", filepath.Join(stage, "work", "link")); err != nil {
		t.Skip(err)
	}
	m := &backup.Manifest{Mode: "server", Build: installrecord.Build{Version: "1", Commit: "test"}, Config: map[string]any{}}
	must(t, backup.Seal(t.Context(), stage, m))
	archive := filepath.Join(t.TempDir(), "a.gz")
	must(t, backup.Pack(t.Context(), stage, archive, m))
	out, _, err := backup.Unpack(t.Context(), archive, t.TempDir())
	must(t, err)
	link, err := os.Readlink(filepath.Join(out, "work", "link"))
	must(t, err)
	if link != "file" {
		t.Fatal("link changed")
	}
	original, err := os.ReadFile(archive)
	must(t, err)
	for _, kind := range []string{"truncated", "checksum", "traversal", "duplicate", "link-parent", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			data := original
			if kind == "truncated" {
				data = data[:len(data)-7]
			} else {
				gz, err := gzip.NewReader(bytes.NewReader(data))
				must(t, err)
				tr := tar.NewReader(gz)
				var buf bytes.Buffer
				zw := gzip.NewWriter(&buf)
				tw := tar.NewWriter(zw)
				for {
					h, err := tr.Next()
					if err == io.EOF {
						break
					}
					must(t, err)
					body, err := io.ReadAll(tr)
					must(t, err)
					if h.Name == "backup.json" && kind == "link-parent" {
						var mm backup.Manifest
						json.Unmarshal(body, &mm)
						for i := range mm.Files {
							if mm.Files[i].Path == "work" {
								mm.Files[i].Kind = "symlink"
								mm.Files[i].Link = "/tmp"
							}
						}
						body, _ = json.Marshal(mm)
						h.Size = int64(len(body))
					}
					if h.Name == "work/file" {
						switch kind {
						case "checksum":
							body = []byte("other")
						case "traversal":
							h.Name = "../escaped"
						}
					}
					must(t, tw.WriteHeader(h))
					_, err = tw.Write(body)
					must(t, err)
					if kind == "duplicate" && h.Name == "work/file" {
						must(t, tw.WriteHeader(h))
						_, err = tw.Write(body)
						must(t, err)
					}
				}
				must(t, tw.Close())
				if kind == "trailing" {
					zw.Write([]byte("trailing"))
				}
				must(t, zw.Close())
				data = buf.Bytes()
			}
			broken := filepath.Join(t.TempDir(), "bad.gz")
			must(t, os.WriteFile(broken, data, 0600))
			if _, _, err := backup.Unpack(t.Context(), broken, t.TempDir()); err == nil {
				t.Fatal("accepted malformed archive")
			}
		})
	}
}

func TestManagedLinksRelocatedWithoutChangingExternalReferences(t *testing.T) {
	stage := t.TempDir()
	must(t, os.Mkdir(filepath.Join(stage, "work"), 0700))
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(t.TempDir(), "target")
	m := &backup.Manifest{DataDir: source, Build: installrecord.Build{OS: runtime.GOOS}}
	links := map[string]string{"internal": filepath.Join(source, "sandbox", "work", "assistant", "file"), "relative": "../file", "external": filepath.Join(t.TempDir(), "unmanaged")}
	for name, link := range links {
		if err := os.Symlink(link, filepath.Join(stage, "work", name)); err != nil {
			t.Skip(err)
		}
		m.Files = append(m.Files, backup.Entry{Path: "work/" + name, Kind: "symlink", Link: link})
	}
	work := target + "-work"
	must(t, backup.RelocateLinks(stage, m, target, work))
	for name, want := range links {
		if name == "internal" {
			want = filepath.Join(work, "assistant", "file")
		}
		got, err := os.Readlink(filepath.Join(stage, "work", name))
		must(t, err)
		if got != want {
			t.Fatalf("%s: got %s want %s", name, got, want)
		}
	}
}
