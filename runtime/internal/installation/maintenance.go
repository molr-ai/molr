package installation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/installrecord"
)

// Task is an installer-owned receipt, never an executable request.
type Task struct {
	ID         string     `json:"id"`
	Operation  string     `json:"operation"`
	Component  string     `json:"component,omitempty"`
	Target     string     `json:"target,omitempty"`
	State      string     `json:"state"`
	Stage      string     `json:"stage"`
	Downloaded int64      `json:"downloaded,omitempty"`
	Total      int64      `json:"total,omitempty"`
	ErrorCode  string     `json:"error_code,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (t Task) Active() bool { return t.State == "running" }

var taskIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func newTaskID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func taskRoot(r *installrecord.Record) string { return filepath.Join(r.Paths.Cache, "maintenance") }
func taskFile(r *installrecord.Record, id string) string {
	return filepath.Join(taskRoot(r), id+".json")
}
func readTask(r *installrecord.Record, id string) (Task, error) {
	var t Task
	if !taskIDPattern.MatchString(id) {
		return t, fmt.Errorf("invalid task ID")
	}
	p := taskFile(r, id)
	if err := checkPath(p); err != nil {
		return t, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return t, err
	}
	if len(b) > 16384 {
		return t, fmt.Errorf("oversized task receipt")
	}
	err = json.Unmarshal(b, &t)
	if err == nil && t.ID != id {
		err = fmt.Errorf("task identity mismatch")
	}
	return t, err
}
func writeTask(r *installrecord.Record, t *Task) error {
	if err := mkdirOwned(taskRoot(r), 0700, r.UID, r.GID); err != nil {
		return err
	}
	t.UpdatedAt = time.Now().UTC()
	return atomicWrite(taskFile(r, t.ID), bytes.NewReader(jsonBytes(t)), 0600, r.UID, r.GID)
}
func (e *Engine) acquire(r *installrecord.Record) error {
	if e.lock != nil {
		return nil
	}
	if e.Options.LockFD != 0 {
		if e.Options.LockFD != 3 {
			return fmt.Errorf("invalid inherited lock")
		}
		f := os.NewFile(3, "setup-lock")
		a, err := f.Stat()
		if err != nil {
			return err
		}
		b, err := os.Lstat(filepath.Join(r.Paths.Program, ".setup.lock"))
		if err != nil || !os.SameFile(a, b) || !a.Mode().IsRegular() {
			f.Close()
			return fmt.Errorf("invalid lock handoff")
		}
		// Taking the same open-file-description's lock is idempotent. A fabricated
		// descriptor must still acquire the real lock, so this cannot bypass exclusion.
		if err = unix.Flock(3, unix.LOCK_EX|unix.LOCK_NB); err != nil {
			f.Close()
			return err
		}
		unix.CloseOnExec(3)
		e.lock = f
		return nil
	}
	f, err := lockRecord(r)
	if e.Options.TaskID != "" {
		deadline := time.Now().Add(10 * time.Second)
		for err != nil && time.Now().Before(deadline) && (errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)) {
			select {
			case <-e.Ctx.Done():
				return e.Ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
			f, err = lockRecord(r)
		}
	}
	if err != nil {
		return err
	}
	e.lock = f
	return nil
}
func lockRecord(r *installrecord.Record) (*os.File, error) {
	// Root owns system installation directories. The service group may read/lock
	// this fixed inode but cannot replace it or change installation executables.
	if !r.System {
		return lockInstance(r.Paths.Program)
	}
	d, err := openDir(r.Paths.Program, true)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if os.Geteuid() == r.OwnerUID {
		flags = unix.O_RDWR | unix.O_CREAT | unix.O_NOFOLLOW | unix.O_CLOEXEC
	}
	fd, err := unix.Openat(int(d.Fd()), ".setup.lock", flags, 0640)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), ".setup.lock")
	fail := func(err error) (*os.File, error) { f.Close(); return nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || st.Nlink != 1 || int(st.Uid) != r.OwnerUID || info.Mode().Perm()&0037 != 0 {
		return fail(fmt.Errorf("unsafe setup lock"))
	}
	if os.Geteuid() == r.OwnerUID && r.GID >= 0 {
		if err = f.Chown(r.OwnerUID, r.GID); err != nil {
			return fail(err)
		}
		if err = f.Chmod(0640); err != nil {
			return fail(err)
		}
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fail(err)
	}
	return f, nil
}
func (e *Engine) beginTask(r *installrecord.Record, operation, component, target string) error {
	if e.task != nil {
		return nil
	}
	if err := e.acquire(r); err != nil {
		return err
	}
	if e.Options.TaskID != "" && !taskIDPattern.MatchString(e.Options.TaskID) {
		return fmt.Errorf("invalid task ID")
	}
	id := e.Options.TaskID
	if id == "" {
		id = newTaskID()
	}
	if old, err := readTask(r, id); err == nil {
		if old.Active() && old.Operation == operation && old.Component == component && old.Target == target && (e.Options.LockFD == 3 || old.Stage == "starting") {
			e.task = &old
			e.taskRecord = r
			return e.progress("checking", 0, 0)
		}
		return fmt.Errorf("task already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Holding the instance lock proves that earlier running receipts have no writer.
	entries, _ := os.ReadDir(taskRoot(r))
	for _, entry := range entries {
		n := entry.Name()
		if len(n) != 37 || n[32:] != ".json" {
			continue
		}
		old, err := readTask(r, n[:32])
		if err == nil && old.Active() {
			if old.Stage == "starting" && (time.Since(old.StartedAt) < 15*time.Second || unitActive(e.Ctx, r, old.ID)) {
				return componentError("maintenance_busy", "another maintenance worker is starting")
			}
			now := time.Now().UTC()
			old.State = "interrupted"
			old.ErrorCode = "maintenance_interrupted"
			old.FinishedAt = &now
			if err = writeTask(r, &old); err != nil {
				return err
			}
		}
	}
	now := time.Now().UTC()
	e.task = &Task{ID: id, Operation: operation, Component: component, Target: target, State: "running", Stage: "checking", StartedAt: now}
	e.taskRecord = r
	if err := writeTask(r, e.task); err != nil {
		return err
	}
	if e.Options.JSON {
		_ = json.NewEncoder(e.machineOut).Encode(e.task)
	}
	return nil
}
func (e *Engine) progress(stage string, downloaded, total int64) error {
	if e.task == nil {
		return nil
	}
	phase, err := lockTaskPhase(e.taskRecord, e.task.ID)
	if err != nil {
		return err
	}
	defer phase.Close()
	if e.task.Stage != "installing" && e.task.Stage != "restarting" {
		if _, err := os.Lstat(filepath.Join(taskRoot(e.taskRecord), e.task.ID+".cancel")); err == nil {
			return context.Canceled
		}
		if err := e.Ctx.Err(); err != nil {
			return err
		}
	}
	e.UI.Stage = stage
	e.task.Stage = stage
	e.task.Downloaded = downloaded
	e.task.Total = total
	return writeTask(e.taskRecord, e.task)
}
func (e *Engine) finishTask(err error) error {
	if e.task == nil {
		return nil
	}
	if e.handedOff {
		if e.Options.JSON {
			if t, readErr := readTask(e.taskRecord, e.task.ID); readErr == nil {
				_ = json.NewEncoder(e.machineOut).Encode(t)
			}
		}
		return nil
	}
	now := time.Now().UTC()
	e.task.FinishedAt = &now
	e.task.State = "succeeded"
	if err == nil {
		e.task.Stage = "complete"
	}
	if err != nil {
		e.task.State = "failed"
		e.task.ErrorCode = "maintenance_failed"
		var pending *VerificationPending
		if errors.As(err, &pending) {
			e.task.ErrorCode = "maintenance_verification_pending"
		}
		var coded *ComponentError
		if errors.As(err, &coded) {
			e.task.ErrorCode = coded.Code
		}
		if errors.Is(err, context.Canceled) {
			e.task.State = "cancelled"
			e.task.ErrorCode = "maintenance_cancelled"
		}
	}
	if writeErr := writeTask(e.taskRecord, e.task); writeErr != nil {
		return fmt.Errorf("cannot save maintenance result: %w", writeErr)
	}
	if e.Options.JSON {
		_ = json.NewEncoder(e.machineOut).Encode(e.task)
	}
	return nil
}

// Cancellation and the start of a commit have one linearization point, separate
// from the instance lock which the worker holds throughout the operation.
func lockTaskPhase(r *installrecord.Record, id string) (*os.File, error) {
	if !taskIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid task ID")
	}
	dir, err := openDir(taskRoot(r), false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), id+".phase", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "task-phase")
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	st := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || st.Nlink != 1 || info.Mode().Perm()&0077 != 0 || (int(st.Uid) != r.UID && int(st.Uid) != r.OwnerUID) {
		f.Close()
		return nil, fmt.Errorf("unsafe task phase lock")
	}
	if os.Geteuid() == 0 {
		if err = f.Chown(r.UID, r.GID); err != nil {
			f.Close()
			return nil, err
		}
	}
	if err = unix.Flock(fd, unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func requestTaskCancel(r *installrecord.Record, id string) (Task, error) {
	phase, err := lockTaskPhase(r, id)
	if err != nil {
		return Task{}, err
	}
	defer phase.Close()
	t, err := readTask(r, id)
	if err != nil || !t.Active() {
		return t, err
	}
	if t.Stage == "installing" || t.Stage == "restarting" {
		return t, componentError("maintenance_not_cancellable", "commit in progress")
	}
	err = atomicWrite(filepath.Join(taskRoot(r), id+".cancel"), bytes.NewReader([]byte("cancel\n")), 0600, r.UID, r.GID)
	return t, err
}
func (e *Engine) downloadContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(e.Ctx, 15*time.Minute)
	if e.task != nil {
		id, record := e.task.ID, e.taskRecord
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, err := os.Lstat(filepath.Join(taskRoot(record), id+".cancel")); err == nil {
						cancel()
						return
					}
				}
			}
		}()
	}
	return ctx, cancel
}
