package installation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/instancebackup"
	"molr.ai/runtime/internal/storage/sqlite"
)

func backupFixture(t *testing.T) (*Engine, *installrecord.Record, *bool, *[]string) {
	t.Helper()
	e, r := fixture(t)
	e.Root = r.Paths.Program
	e.Build = buildinfo.Info{Version: r.Build.Version, Commit: r.Build.Commit, OS: r.Build.OS, Arch: r.Build.Arch}
	e.Options.Port = 18787
	e.Options.ProgramRoot = r.ProgramRoot
	e.Options.Permissions = "fix"
	e.Options.NoStart = true
	e.UI.Yes = true
	cfg := config.Defaults(r.DataDir)
	cfg.Sandbox.Enabled = false
	cfg.HTTP.Listen = "127.0.0.1:18787"
	cfg.HTTP.PublicURL = "http://127.0.0.1:18787"
	cfg.HTTP.TLSMode = config.LocalHTTP
	if err := os.WriteFile(filepath.Join(r.Paths.Config, "server.json"), jsonBytes(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(r.DataDir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	os.MkdirAll(filepath.Join(r.DataDir, "keys"), 0700)
	os.WriteFile(filepath.Join(r.DataDir, "keys", "master.key"), make([]byte, 32), 0600)
	os.WriteFile(filepath.Join(r.DataDir, "retained.txt"), []byte("before"), 0600)
	exe, _ := os.Executable()
	os.MkdirAll(filepath.Join(r.Paths.Program, "bin"), 0755)
	for _, name := range []string{"molr", "molr-setup"} {
		if err = copyFile(filepath.Join(r.Paths.Program, "bin", name), exe, 0755, r.UID, r.GID); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md"} {
		os.WriteFile(filepath.Join(r.Paths.Program, name), []byte("fixture"), 0644)
	}
	os.WriteFile(filepath.Join(r.Paths.Program, "release.json"), jsonBytes(r.Build), 0644)
	active := true
	calls := []string{}
	e.Command = func(_ context.Context, args []string, _ io.Reader, _ []string, _, _ io.Writer) ([]byte, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, "systemctl --version") {
			return []byte("systemd 257"), nil
		}
		if strings.Contains(joined, " show ") {
			state, pid := "inactive", "0"
			if active {
				state, pid = "active", "123"
			}
			return []byte("LoadState=loaded\nActiveState=" + state + "\nMainPID=" + pid + "\n"), nil
		}
		if strings.Contains(joined, " stop ") {
			active = false
		}
		if strings.Contains(joined, " start ") || strings.Contains(joined, " restart ") {
			active = true
		}
		if strings.Contains(joined, "version --json") {
			return json.Marshal(r.Build)
		}
		return nil, nil
	}
	return e, r, &active, &calls
}
func TestBackupStopsCopiesAndRestarts(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprint(keep), func(t *testing.T) {
			e, r, active, calls := backupFixture(t)
			e.Options.Output = filepath.Join(t.TempDir(), "backup.tar.gz")
			e.Options.KeepStopped = keep
			if err := e.backupInstance(r); err != nil {
				t.Fatal(err)
			}
			if *active == keep {
				t.Fatal("wrong final service state")
			}
			if len(*calls) < 2 {
				t.Fatal("did not control service")
			}
			if st, err := os.Stat(e.Options.Output); err != nil || st.Mode().Perm() != 0600 {
				t.Fatal("missing/private archive", err)
			}
		})
	}
}
func TestBackupFailureResumesAndStoppedStaysStopped(t *testing.T) {
	for _, activeBefore := range []bool{true, false} {
		t.Run(fmt.Sprint(activeBefore), func(t *testing.T) {
			e, r, active, _ := backupFixture(t)
			*active = activeBefore
			e.Options.Output = filepath.Join(t.TempDir(), "backup.tar.gz")
			os.Remove(filepath.Join(r.Paths.Program, "LICENSE"))
			if err := e.backupInstance(r); err == nil {
				t.Fatal("incomplete backup succeeded")
			}
			if *active != activeBefore {
				t.Fatal("service state changed after failure")
			}
			if _, err := os.Stat(e.Options.Output); !os.IsNotExist(err) {
				t.Fatal("partial archive published")
			}
		})
	}
}
func TestRestoreOverwriteKeepsRecoveryAndRejectsVersionMismatch(t *testing.T) {
	e, r, active, _ := backupFixture(t)
	e.Options.Output = filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := e.backupInstance(r); err != nil {
		t.Fatal(err)
	}
	e.lock.Close()
	e.lock = nil
	e.Options.Archive = e.Options.Output
	e.Options.Output = ""
	e.Options.Command = "restore"
	os.WriteFile(filepath.Join(r.DataDir, "retained.txt"), []byte("after"), 0600)
	if err := e.restoreInstance(r); err == nil || !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("implicit overwrite: %v", err)
	}
	e.Options.Overwrite = true
	e.Build.Commit = strings.Repeat("b", 40)
	if err := e.restoreInstance(r); err == nil {
		t.Fatal("accepted wrong version")
	}
	e.Build.Commit = r.Build.Commit
	if err := e.restoreInstance(r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(r.DataDir, "retained.txt"))
	if err != nil || string(b) != "before" {
		t.Fatal("data not restored", err)
	}
	if *active {
		t.Fatal("--no-start ignored")
	}
	backups, _ := filepath.Glob(filepath.Join(r.Paths.Cache, "before-restore-*.tar.gz"))
	if len(backups) != 1 {
		t.Fatal("missing recovery backup")
	}
}
func TestRestoreInvalidArchiveDoesNotStopService(t *testing.T) {
	e, r, active, calls := backupFixture(t)
	e.Options.Archive = filepath.Join(t.TempDir(), "bad.gz")
	os.WriteFile(e.Options.Archive, []byte("broken"), 0600)
	e.Options.Overwrite = true
	if err := e.restoreInstance(r); err == nil {
		t.Fatal("accepted corrupt archive")
	}
	if !*active || len(*calls) != 0 {
		t.Fatal("corrupt archive affected target")
	}
}

func TestRestoreNewInstanceAndForeignPrograms(t *testing.T) {
	e, r, _, _ := backupFixture(t)
	e.Options.Output = filepath.Join(t.TempDir(), "source.tar.gz")
	if err := e.backupInstance(r); err != nil {
		t.Fatal(err)
	}
	e.lock.Close()
	e.lock = nil
	stage, m, err := instancebackup.Unpack(t.Context(), e.Options.Output, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.Build.OS = "darwin"
	m.Build.Arch = "arm64"
	if err = os.WriteFile(filepath.Join(stage, "program", "bin", "molr"), []byte("foreign binary must never run"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = instancebackup.Seal(t.Context(), stage, m); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "foreign.tar.gz")
	if err = instancebackup.Pack(t.Context(), stage, archive, m); err != nil {
		t.Fatal(err)
	}
	originalCommand := e.Command
	e.Command = func(ctx context.Context, args []string, in io.Reader, env []string, out, diag io.Writer) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "show molr-migrated.service") {
			return []byte("LoadState=not-found\nMainPID=0\nActiveState=inactive\n"), nil
		}
		return originalCommand(ctx, args, in, env, out, diag)
	}
	e.Options.Command = "restore"
	e.Options.Archive = archive
	e.Options.Name = "molr-migrated"
	e.Options.NameSet = true
	if err = e.restoreInstance(nil); err != nil {
		t.Fatal(err)
	}
	targetPaths, _, err := layout(e.Options.Name, r.Home, false, r.ProgramRoot)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := readRecord(filepath.Join(targetPaths.Program, "installation.json"), true)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Build != r.Build {
		t.Fatal("foreign build selected")
	}
	b, err := os.ReadFile(filepath.Join(restored.DataDir, "retained.txt"))
	if err != nil || string(b) != "before" {
		t.Fatal("new instance data lost", err)
	}
	if err = checkExecutable(filepath.Join(restored.Paths.Program, "bin", "molr"), r.Build.Arch); err != nil {
		t.Fatal("foreign program installed", err)
	}
}

func TestRestoreApplyFailureRollsBackData(t *testing.T) {
	e, r, active, _ := backupFixture(t)
	e.Options.Output = filepath.Join(t.TempDir(), "source.tar.gz")
	if err := e.backupInstance(r); err != nil {
		t.Fatal(err)
	}
	e.lock.Close()
	e.lock = nil
	os.WriteFile(filepath.Join(r.DataDir, "retained.txt"), []byte("target before restore"), 0600)
	originalCommand := e.Command
	failed := false
	e.Command = func(ctx context.Context, args []string, in io.Reader, env []string, out, diag io.Writer) ([]byte, error) {
		if !failed && strings.Contains(strings.Join(args, " "), "daemon-reload") {
			failed = true
			return nil, fmt.Errorf("injected service registration failure")
		}
		return originalCommand(ctx, args, in, env, out, diag)
	}
	e.Options.Archive = e.Options.Output
	e.Options.Command = "restore"
	e.Options.Overwrite = true
	if err := e.restoreInstance(r); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("missing injected failure: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(r.DataDir, "retained.txt"))
	if err != nil || string(b) != "target before restore" {
		t.Fatal("rollback lost original target data", err)
	}
	if !*active {
		t.Fatal("rollback did not resume service")
	}
}
