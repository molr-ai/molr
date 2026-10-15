//go:build linux

package installation

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type componentTransportFunc func(*http.Request) (*http.Response, error)

func (f componentTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestComponentDownloadChecksDigestSizeAndSource(t *testing.T) {
	for _, mode := range []string{"valid", "digest", "size", "source", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			e, _ := fixture(t)
			data := []byte("trusted executable")
			sum := sha256.Sum256(data)
			a := componentAsset{URL: "https://github.com/fixture", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
			calls := 0
			e.componentTransport = componentTransportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				res := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Request: req}
				if mode == "redirect" {
					res.StatusCode = 302
					res.Header.Set("Location", "https://attacker.invalid/payload")
				}
				return res, nil
			})
			switch mode {
			case "digest":
				a.SHA256 = strings.Repeat("0", 64)
			case "size":
				a.Size--
			case "source":
				a.URL = "https://attacker.invalid/payload"
			}
			err := e.fetchComponent(a, filepath.Join(t.TempDir(), "download"))
			if (err == nil) != (mode == "valid") {
				t.Fatalf("%s: %v", mode, err)
			}
			if mode == "source" && calls != 0 {
				t.Fatal("unapproved host contacted")
			}
			if mode == "redirect" && calls != 1 {
				t.Fatal("unapproved redirect followed")
			}
		})
	}
}
func componentArchive(t *testing.T, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "component.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	h := &zip.FileHeader{Name: name}
	h.SetMode(mode)
	w, err := z.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("test"))
	z.Close()
	f.Close()
	return p
}
func TestComponentArchiveRejectsEscapesLinksAndLimits(t *testing.T) {
	for _, name := range []string{"root/../escape", "/escape", "root/../../escape", "root/x\\y", "root/./chrome"} {
		p := componentArchive(t, name, 0755)
		if err := extractComponent(p, t.TempDir(), componentAsset{Prefix: "root/", Executable: "chrome", Expanded: 100}); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	for _, mode := range []os.FileMode{os.ModeSymlink | 0777, os.ModeNamedPipe | 0600} {
		p := componentArchive(t, "root/chrome", mode)
		if err := extractComponent(p, t.TempDir(), componentAsset{Prefix: "root/", Executable: "chrome", Expanded: 100}); err == nil {
			t.Fatal("special entry accepted")
		}
	}
	p := componentArchive(t, "root/chrome", 0755)
	if err := extractComponent(p, t.TempDir(), componentAsset{Prefix: "root/", Executable: "chrome", Expanded: 3}); err == nil {
		t.Fatal("expanded limit bypassed")
	}
	root := t.TempDir()
	if err := extractComponent(p, root, componentAsset{Prefix: "root/", Executable: "chrome", Expanded: 100}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(root, "chrome"))
	if info.Mode().Perm() != 0755 {
		t.Fatal(info.Mode())
	}
}
func TestMaintenanceLockSerializesCLIAndRuntime(t *testing.T) {
	e, r := fixture(t)
	if err := e.beginTask(r, "component_install", "browser", "fixture"); err != nil {
		t.Fatal(err)
	}
	if lock, err := lockRecord(r); err == nil {
		lock.Close()
		t.Fatal("concurrent lock accepted")
	}
	saved, err := readTask(r, e.task.ID)
	if err != nil || !saved.Active() {
		t.Fatal(saved, err)
	}
	e.finishTask(nil)
	e.lock.Close()
	e.lock = nil
	lock, err := lockRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	saved, err = readTask(r, saved.ID)
	if err != nil || saved.State != "succeeded" {
		t.Fatal(saved, err)
	}
}
func TestMaintenanceRecoversInterruptedReceiptAndCancellation(t *testing.T) {
	e, r := fixture(t)
	if err := e.beginTask(r, "component_install", "browser", "fixture"); err != nil {
		t.Fatal(err)
	}
	id := e.task.ID
	e.lock.Close()
	e.lock = nil
	e.task = nil
	if err := e.beginTask(r, "component_repair", "browser", "fixture"); err != nil {
		t.Fatal(err)
	}
	old, err := readTask(r, id)
	if err != nil || old.State != "interrupted" {
		t.Fatal(old, err)
	}
	if err = os.WriteFile(filepath.Join(taskRoot(r), e.task.ID+".cancel"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.progress("downloading", 0, 100); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	e.task.Stage = "installing"
	if err = e.progress("restarting", 0, 0); err != nil {
		t.Fatal("cancel interrupted commit", err)
	}
}
func TestMaintenanceAcceptsReservedTaskWithoutLosingIdentity(t *testing.T) {
	e, r := fixture(t)
	id := newTaskID()
	task := Task{ID: id, Operation: "component_install", Component: "browser", Target: "fixture", State: "running", Stage: "starting", StartedAt: time.Now().UTC()}
	if err := writeTask(r, &task); err != nil {
		t.Fatal(err)
	}
	e.Options.TaskID = id
	if err := e.beginTask(r, task.Operation, task.Component, task.Target); err != nil {
		t.Fatal(err)
	}
	saved, err := readTask(r, id)
	if err != nil || saved.Stage != "checking" {
		t.Fatal(saved, err)
	}
}
func TestTaskIdentifiersAndUnsafeLockAreRejected(t *testing.T) {
	_, r := fixture(t)
	if _, err := readTask(r, "../outside"); err == nil {
		t.Fatal("unsafe ID accepted")
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "target"), filepath.Join(r.Paths.Program, ".setup.lock")); err != nil {
		t.Fatal(err)
	}
	if f, err := lockRecord(r); err == nil {
		f.Close()
		t.Fatal("symlink lock accepted")
	}
}

func TestUnmanagedMaintenanceFailsClosed(t *testing.T) {
	m := NewMaintenance(filepath.Join(t.TempDir(), "server.json"), false)
	if m.CanUpdate(t.Context()) || m.Enabled() || len(m.ProtectedRoots()) != 0 || m.Latest(t.Context()) != nil {
		t.Fatal("unmanaged installer enabled")
	}
	if _, _, err := m.Programs(); err == nil {
		t.Fatal("unmanaged programs accepted")
	}
	s := m.Snapshot(t.Context())
	if len(s) != 1 || s[0].CanInstall || s[0].Reason != "maintenance_not_installed" {
		t.Fatal(s)
	}
	if _, err := m.Start(t.Context(), "install", "browser", newTaskID(), ""); err == nil {
		t.Fatal("unmanaged task accepted")
	}
	if _, err := m.Task(t.Context(), newTaskID()); err == nil {
		t.Fatal("unmanaged task read accepted")
	}
	if _, err := m.Cancel(t.Context(), newTaskID()); err == nil {
		t.Fatal("unmanaged cancellation accepted")
	}
}

func TestCancellationRechecksPhaseAfterCommitWins(t *testing.T) {
	e, r := fixture(t)
	if err := e.beginTask(r, "component_install", "browser", "fixture"); err != nil {
		t.Fatal(err)
	}
	phase, err := lockTaskPhase(r, e.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := requestTaskCancel(r, e.task.ID); done <- err }()
	e.task.Stage = "installing"
	if err = writeTask(r, e.task); err != nil {
		t.Fatal(err)
	}
	phase.Close()
	err = <-done
	var coded *ComponentError
	if !errors.As(err, &coded) || coded.Code != "maintenance_not_cancellable" {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(taskRoot(r), e.task.ID+".cancel")); !os.IsNotExist(err) {
		t.Fatal("cancellation accepted after commit", err)
	}
}
func TestMaintenanceReceiptFailureIsNotReportedAsSuccess(t *testing.T) {
	e, r := fixture(t)
	if err := e.beginTask(r, "component_install", "browser", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(taskRoot(r)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskRoot(r), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.finishTask(nil); err == nil {
		t.Fatal("receipt failure ignored")
	}
}

func TestMaintenanceDoesNotOverwriteCorruptReceipt(t *testing.T) {
	e, r := fixture(t)
	e.Options.TaskID = newTaskID()
	if err := mkdirOwned(taskRoot(r), 0700, r.UID, r.GID); err != nil {
		t.Fatal(err)
	}
	file := taskFile(r, e.Options.TaskID)
	if err := os.WriteFile(file, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.beginTask(r, "component_install", "browser", "fixture"); err == nil {
		t.Fatal("accepted corrupt receipt")
	}
	if e.lock != nil {
		e.lock.Close()
	}
	data, _ := os.ReadFile(file)
	if string(data) != "corrupt" {
		t.Fatal("overwrote receipt")
	}
}
