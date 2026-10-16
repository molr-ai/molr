//go:build linux

package installation

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/installrecord"
)

// Maintenance only launches the installed setup binary; it never installs in-process.
type Maintenance struct {
	record   *installrecord.Record
	external bool
	mu       sync.Mutex
}

func NewMaintenance(configPath string, external bool) *Maintenance {
	m := &Maintenance{external: external}
	exe, err := os.Executable()
	if err != nil {
		return m
	}
	root := filepath.Dir(filepath.Dir(exe))
	r, err := readRecord(filepath.Join(root, "installation.json"), false)
	if err == nil && r.State == "installed" && r.Paths.Program == root && filepath.Join(r.Paths.Config, "server.json") == configPath {
		m.record = r
	}
	return m
}
func (m *Maintenance) ProtectedRoots() []string {
	if m == nil || m.record == nil {
		return nil
	}
	return []string{m.record.Paths.Program, m.record.Paths.Config, m.record.Paths.Cache}
}
func (m *Maintenance) Programs() (string, string, error) {
	if m == nil || m.record == nil {
		return "", "", componentError("maintenance_not_installed", "installed molr-setup required")
	}
	return componentPrograms(m.record)
}
func (m *Maintenance) Enabled() bool {
	if m == nil || m.record == nil || m.external {
		return false
	}
	_, err := os.Stat(filepath.Join(componentRoot(m.record), "browser.enabled"))
	return err == nil
}
func unitName(r *installrecord.Record, id string) string {
	return "molr-maintenance-" + r.Name + "-" + id + ".service"
}
func unitActive(ctx context.Context, r *installrecord.Record, id string) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show", unitName(r, id), "--property=ActiveState", "--value")
	b, err := cmd.Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(b))
	return s == "active" || s == "activating" || s == "deactivating"
}
func runnerAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show-environment")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}
func (m *Maintenance) available(ctx context.Context, update bool) error {
	if m == nil || m.record == nil {
		return componentError("maintenance_not_installed", "installed molr-setup required")
	}
	r := m.record
	if update && r.Manager == "desktop" {
		return componentError("maintenance_desktop_managed", "update this Runtime in Molr Desktop")
	}
	uid := os.Geteuid()
	if uid != r.UID {
		return componentError("maintenance_permission_denied", "service account required")
	}
	if update && (r.System || uid != r.OwnerUID) {
		return componentError("maintenance_admin_cli_required", "system installation updates require administrator CLI")
	}
	setup := filepath.Join(r.Paths.Program, "bin/molr-setup")
	if err := checkManaged(setup, r.OwnerUID, nil); err != nil {
		return componentError("maintenance_not_installed", err.Error())
	}
	if err := staticELF(setup, r.Build.Arch); err != nil {
		return componentError("maintenance_not_installed", err.Error())
	}
	if !runnerAvailable(ctx) {
		return componentError("maintenance_runner_unavailable", "user systemd manager unavailable; use molr-setup CLI")
	}
	return nil
}
func (m *Maintenance) Snapshot(ctx context.Context) []ComponentStatus {
	s := ComponentStatus{ID: "browser", State: "missing"}
	if m != nil && m.record != nil {
		s = componentSnapshot(m.record)
		if s.Task != nil {
			if t, err := m.Task(ctx, s.Task.ID); err == nil {
				s.Task = &t
			}
		}
	}
	if m != nil && m.external {
		s.State = "external"
		s.Reason = "component_externally_managed"
		return []ComponentStatus{s}
	}
	if err := m.available(ctx, false); err != nil {
		var e *ComponentError
		if errors.As(err, &e) {
			s.Reason = e.Code
		}
	} else {
		s.CanInstall = s.State != "unsupported"
	}
	return []ComponentStatus{s}
}
func (m *Maintenance) CanUpdate(ctx context.Context) bool { return m.available(ctx, true) == nil }
func (m *Maintenance) Task(ctx context.Context, id string) (Task, error) {
	if m == nil || m.record == nil {
		return Task{}, componentError("maintenance_not_installed", "installed molr-setup required")
	}
	t, err := readTask(m.record, id)
	if err != nil {
		return t, err
	}
	if !t.Active() {
		return t, nil
	}
	lock, err := lockRecord(m.record)
	if err != nil {
		return t, nil
	}
	defer lock.Close()
	t, err = readTask(m.record, id)
	if err != nil {
		return t, err
	}
	if t.Active() && !unitActive(ctx, m.record, id) && !(t.Stage == "starting" && time.Since(t.StartedAt) < 15*time.Second) {
		now := time.Now().UTC()
		t.State = "interrupted"
		t.ErrorCode = "maintenance_interrupted"
		t.FinishedAt = &now
		err = writeTask(m.record, &t)
	}
	return t, err
}
func (m *Maintenance) Start(ctx context.Context, operation, component, key, target string) (Task, error) {
	if !taskIDPattern.MatchString(key) {
		return Task{}, componentError("maintenance_invalid_request", "32-character idempotency key required")
	}
	update := operation == "update" && component == ""
	if !update && (component != "browser" || (operation != "install" && operation != "update" && operation != "repair")) {
		return Task{}, componentError("maintenance_invalid_request", "unknown maintenance action")
	}
	if !update && m.external {
		return Task{}, componentError("component_externally_managed", "explicit browser paths configured")
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.available(probe, update); err != nil {
		return Task{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.record
	if !update {
		release, err := browserRelease()
		if err != nil {
			return Task{}, err
		}
		target = release.Version
	} else if !releaseVersion.MatchString(target) {
		return Task{}, componentError("maintenance_invalid_request", "invalid target version")
	}
	op := operation
	if !update {
		op = "component_" + operation
	}
	if old, err := readTask(r, key); err == nil {
		if old.Operation != op || old.Component != component || old.Target != target {
			return Task{}, componentError("maintenance_invalid_request", "idempotency key reused")
		}
		return old, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Task{}, err
	}
	lock, err := lockRecord(r)
	if err != nil {
		old := latestTask(r)
		if old != nil && old.Active() && old.Operation == op && old.Component == component && old.Target == target {
			return *old, nil
		}
		return Task{}, componentError("maintenance_busy", "another maintenance task holds the instance lock")
	}
	if old := latestTask(r); old != nil && old.Active() && (unitActive(probe, r, old.ID) || old.Stage == "starting" && time.Since(old.StartedAt) < 15*time.Second) {
		lock.Close()
		if old.Operation == op && old.Component == component && old.Target == target {
			return *old, nil
		}
		return Task{}, componentError("maintenance_busy", "another maintenance task is starting")
	}
	now := time.Now().UTC()
	t := Task{ID: key, Operation: op, Component: component, Target: target, State: "running", Stage: "starting", StartedAt: now}
	if err = writeTask(r, &t); err != nil {
		lock.Close()
		return t, err
	}
	args := []string{"--user", "--quiet", "--collect", "--unit=" + unitName(r, key), "--property=Type=exec", "--property=UMask=0077", "--property=RuntimeMaxSec=1800", "--expand-environment=no", filepath.Join(r.Paths.Program, "bin/molr-setup")}
	if update {
		args = append(args, "update", "--version", target, "--skip-dependencies")
	} else {
		args = append(args, "components", operation, component)
	}
	// Forward only administrator-provided networking settings. Values are inherited
	// by systemd-run, never put into the command line or HTTP task receipts.
	var environment []string
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if os.Getenv(name) != "" {
			environment = append(environment, "--setenv="+name)
		}
	}
	args = append(environment, args...)
	args = append(args, "--record", r.File(), "--non-interactive", "--json", "--task-id", key)
	startCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer stop()
	cmd := exec.CommandContext(startCtx, "systemd-run", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err = cmd.Run()
	lock.Close()
	if err != nil {
		t.State = "failed"
		t.ErrorCode = "maintenance_runner_unavailable"
		now = time.Now().UTC()
		t.FinishedAt = &now
		_ = writeTask(r, &t)
		return t, componentError(t.ErrorCode, "cannot start maintenance service")
	}
	// The worker must acquire the lock and acknowledge the receipt before HTTP 202.
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		v, readErr := readTask(r, key)
		if readErr == nil && v.Stage != "starting" {
			return v, nil
		}
		select {
		case <-startCtx.Done():
			return t, componentError("maintenance_start_timeout", "maintenance worker did not accept the task")
		case <-timer.C:
		}
	}
}
func (m *Maintenance) Cancel(ctx context.Context, id string) (Task, error) {
	t, err := m.Task(ctx, id)
	if err != nil || !t.Active() {
		return t, err
	}
	return requestTaskCancel(m.record, id)
}
func (m *Maintenance) Latest(ctx context.Context) *Task {
	if m == nil || m.record == nil {
		return nil
	}
	t := latestTask(m.record)
	if t == nil {
		return nil
	}
	v, err := m.Task(ctx, t.ID)
	if err != nil {
		return nil
	}
	return &v
}
