//go:build linux || darwin

package installation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/instancebackup"
	"molr.ai/runtime/internal/platform"
)

func (e *Engine) backupTarget() (*installrecord.Record, error) {
	if e.Options.Record != "" {
		return readRecord(e.Options.Record, true)
	}
	if _, err := os.Lstat(filepath.Join(e.Root, "installation.json")); err == nil {
		r, err := readRecord(filepath.Join(e.Root, "installation.json"), true)
		if err != nil {
			return nil, err
		}
		if !e.Options.NameSet || e.Options.Name == r.Name {
			return r, nil
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	r, err := e.locate()
	if os.IsNotExist(err) {
		return nil, nil
	}
	return r, err
}
func (e *Engine) backupCommand() error {
	e.UI.Stage = "Inspect backup or restore request"
	r, err := e.backupTarget()
	if err != nil {
		return err
	}
	if e.Options.Command == "backup" {
		if r == nil {
			return fmt.Errorf("no managed instance found; select --record or --name")
		}
		return e.backupInstance(r)
	}
	return e.restoreInstance(r)
}
func running(state map[string]string) bool { return state["MainPID"] != "" && state["MainPID"] != "0" }
func (e *Engine) resumeBackup(r *installrecord.Record) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.Ctx), 90*time.Second)
	defer cancel()
	worker := *e
	worker.Ctx = ctx
	return worker.resumeService(r)
}
func (e *Engine) backupInstance(r *installrecord.Record) (resultErr error) {
	output, err := filepath.Abs(e.Options.Output)
	if err != nil {
		return err
	}
	if err = checkPath(output); err != nil {
		return err
	}
	if _, err = os.Lstat(output); err == nil {
		return fmt.Errorf("backup destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(r.Paths.Config, "server.json"))
	if err != nil {
		return err
	}
	cfg, err := config.Load(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	for _, p := range []string{r.DataDir, r.Paths.Config, r.Paths.Program, cfg.Sandbox.WorkRoot} {
		if p != "" && overlaps(output, p) {
			return fmt.Errorf("backup destination must be outside backed-up paths")
		}
	}
	e.UI.print("Backup instance: %s\nMode: %s\nArchive: %s\nThe service will stop while files are copied.", r.Name, r.Mode, output)
	if e.Options.DryRun {
		return nil
	}
	if err = e.acquire(r); err != nil {
		return err
	}
	current, err := readRecord(r.File(), true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, r) {
		return fmt.Errorf("installation changed during backup planning")
	}
	stage, err := os.MkdirTemp("", "molr-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, p := range []string{r.DataDir, r.Paths.Config, r.Paths.Program, cfg.Sandbox.WorkRoot} {
		if p != "" && overlaps(stage, p) {
			return fmt.Errorf("TMPDIR must be outside backup sources")
		}
	}
	if err = backupSpace(r, cfg, stage, filepath.Dir(output)); err != nil {
		return err
	}
	state, err := e.unitState(r)
	if err != nil {
		return err
	}
	e.UI.Stage = "Copy offline instance"
	e.UI.State = "Source service may be stopped; failure attempts to restore its original running state."
	resume := running(state)
	if err = e.stopService(r, state, false); err != nil {
		return err
	}
	defer func() {
		if resume {
			resultErr = errors.Join(resultErr, e.resumeBackup(r))
		}
	}()
	lock, err := platform.NewLocker().Acquire(e.Ctx, r.DataDir)
	if err != nil {
		return err
	}
	m, err := instancebackup.Capture(e.Ctx, stage, *r)
	closeErr := lock.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if resume && !e.Options.KeepStopped {
		err = e.resumeBackup(r)
		resume = false
		if err != nil {
			return err
		}
	}
	if err = instancebackup.Pack(e.Ctx, stage, output, m); err != nil {
		return err
	}
	resume = false
	e.UI.State = "Backup archive published; requested source service state applied."
	e.UI.print("Backup complete: %s\nExternal directories excluded: %d", output, len(m.Excluded))
	return nil
}

func (e *Engine) restoreInstance(previous *installrecord.Record) error {
	e.UI.Stage = "Validate archive and plan restore"
	e.UI.State = "Target instance has not been changed."
	stage, m, err := instancebackup.Unpack(e.Ctx, e.Options.Archive, "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = instancebackup.CheckDatabase(e.Ctx, stage, m); err != nil {
		return err
	}
	if previous != nil && !e.Options.Overwrite {
		return fmt.Errorf("instance %s exists; select --overwrite or a new --name/--program-root", previous.Name)
	}
	if previous != nil && previous.Mode != m.Mode {
		return fmt.Errorf("cannot restore %s backup over %s instance", m.Mode, previous.Mode)
	}
	if e.Options.Mode != "" && e.Options.Mode != m.Mode {
		return fmt.Errorf("backup mode does not match --mode")
	}
	if m.Build.Version != e.Build.Version || m.Build.Commit != e.Build.Commit {
		return fmt.Errorf("restore needs molr-setup %s (%s); backup programs can be used on %s/%s", m.Build.Version, m.Build.Commit, m.Build.OS, m.Build.Arch)
	}
	packageRoot := filepath.Join(stage, "program")
	if m.Build.OS != runtime.GOOS || m.Build.Arch != runtime.GOARCH {
		packageRoot = e.Root
		e.UI.print("Source platform %s/%s differs; ignoring archived programs and using this platform's package.", m.Build.OS, m.Build.Arch)
	}
	pkg, err := e.packageAt(packageRoot)
	if err != nil {
		return err
	}
	saved := e.Options
	e.Options.Mode = m.Mode
	e.Options.Config = ""
	e.Options.NoStart = true
	e.Options.Preset = "least-privilege"
	p, err := e.plan(pkg, previous)
	e.Options = saved
	if err != nil {
		return err
	}
	if p.Previous != nil && previous == nil {
		return fmt.Errorf("target installation already exists; select it explicitly with --overwrite")
	}
	work := ""
	if m.Mode == "server" && m.WorkRoot != "" {
		work = p.Record.DataDir + ".work"
	}
	if previous != nil {
		raw, err := os.ReadFile(filepath.Join(previous.Paths.Config, "server.json"))
		if err != nil {
			return err
		}
		oldConfig, err := config.Load(bytes.NewReader(raw))
		if err != nil {
			return err
		}
		if m.Mode == "server" && oldConfig.Sandbox.WorkRoot != "" {
			work = oldConfig.Sandbox.WorkRoot
		}
	}
	if work != "" {
		if err := separate(work, p.Record.DataDir, p.Record.Paths.Program, p.Record.Paths.Config, p.Record.Paths.Cache); err != nil {
			return err
		}
	}

	cfg, notes, err := instancebackup.Configuration(m, p.Record.DataDir, p.Record.Paths.Config, work)
	if err != nil {
		return err
	}
	if e.Options.Port != 0 || e.Options.PublicHost != "" {
		h, _ := cfg["http"].(map[string]any)
		if h["tls_mode"] == "direct" || h["tls_mode"] == "proxy" {
			return fmt.Errorf("TLS/proxy address changes require editing the restored configuration with --no-start")
		}
		cfg["http"], err = defaultHTTP(e.Options.Port, e.Options.PublicHost)
		if err != nil {
			return err
		}
	}
	p.Config = cfg
	s, _ := cfg["sandbox"].(map[string]any)
	enabled, ok := s["enabled"].(bool)
	p.Sandbox = !ok || enabled
	if err = prepareRestoreStage(stage, m, p, work); err != nil {
		return err
	}
	for _, note := range notes {
		e.UI.print("%s", note)
	}
	e.showPlan(p)
	e.UI.print("Restore mode: %s\nOverwrite: %t\nSource version: %s (%s)", m.Mode, previous != nil, m.Build.Version, m.Build.Commit)
	if e.Options.DryRun {
		return nil
	}
	if err = e.resolvePermissions(p.Choices); err != nil {
		return err
	}
	yes, err := e.UI.confirm("Restore this instance backup?")
	if err != nil {
		return err
	}
	if !yes {
		return fmt.Errorf("restore cancelled")
	}
	return e.applyRestore(p, stage, m, work)
}

func prepareRestoreStage(stage string, m *instancebackup.Manifest, p *Plan, work string) error {
	if err := instancebackup.RelocateLinks(stage, m, p.Record.DataDir, work); err != nil {
		return err
	}
	// Restore provenance also preserves foreign/unavailable directory references.
	if err := os.WriteFile(filepath.Join(stage, "config", "restore-source.json"), jsonBytes(m), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "config", "server.json"), jsonBytes(p.Config), 0600); err != nil {
		return err
	}
	if m.Mode == "sandbox" {
		for key, name := range m.References {
			if key != "http.cert_file" && key != "http.key_file" {
				continue
			}
			dest := "restored-" + strings.Split(key, ".")[1]
			if err := copyFile(filepath.Join(stage, "data", dest), filepath.Join(stage, filepath.FromSlash(name)), 0600, os.Geteuid(), os.Getegid()); err != nil {
				return err
			}
		}
	}
	if work != "" && m.WorkRoot == "" {
		defaultWork := filepath.Join(stage, "data", "sandbox", "work")
		if _, err := os.Stat(defaultWork); err == nil {
			if err = os.Rename(defaultWork, filepath.Join(stage, "work")); err != nil {
				return err
			}
		} else if os.IsNotExist(err) {
			if err = os.Mkdir(filepath.Join(stage, "work"), 0700); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if work != "" {
		if st, err := os.Stat(filepath.Join(stage, "work")); err != nil || !st.IsDir() {
			return fmt.Errorf("managed workspace missing from backup")
		}
	}
	return nil
}

type restoreTree struct {
	destination, staged, saved string
	moved, installed           bool
}

func prepareTree(ctx context.Context, source, dest string, uid, gid int) (restoreTree, error) {
	t := restoreTree{destination: dest}
	if err := checkPath(dest); err != nil {
		return t, err
	}
	parent, err := openDir(filepath.Dir(dest), true)
	if err != nil {
		return t, err
	}
	parent.Close()
	root, err := os.MkdirTemp(filepath.Dir(dest), ".molr-restore-tree-")
	if err != nil {
		return t, err
	}
	if err = instancebackup.Add(ctx, root, source, "tree", nil); err != nil {
		os.RemoveAll(root)
		return t, err
	}
	t.staged = filepath.Join(root, "tree")
	t.saved = filepath.Join(root, "previous")
	err = filepath.WalkDir(t.staged, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if os.Geteuid() == 0 {
			return os.Lchown(path, uid, gid)
		}
		return nil
	})
	if err != nil {
		os.RemoveAll(root)
	}
	return t, err
}
func (e *Engine) applyRestore(p *Plan, stage string, m *instancebackup.Manifest, work string) (resultErr error) {
	e.UI.Stage = "Prepare restore"
	e.UI.State = "Preparing target permissions and dependencies; instance data has not been replaced."
	r := p.Record
	if r.UID < 0 {
		return fmt.Errorf("create service account %s before restoring", r.User)
	}
	if err := e.acquire(r); err != nil {
		return err
	}
	if p.Previous != nil {
		current, err := readRecord(r.File(), true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, p.Previous) {
			return fmt.Errorf("target installation changed")
		}
	} else if _, err := os.Lstat(r.File()); err == nil {
		return fmt.Errorf("target installed since planning")
	}
	for _, c := range sortedChoices(p.Choices) {
		if err := applyPermission(c); err != nil {
			return err
		}
	}
	if err := e.prepareDependencies(p.Sandbox); err != nil {
		return err
	}
	delegate, err := e.checkServiceHost(r, p.Sandbox)
	if err != nil {
		return err
	}
	if p.Sandbox {
		if err = e.preparePolicy(r); err != nil {
			return err
		}
	}
	for _, name := range []string{"molr", "molr-setup"} {
		if err = checkExecutable(filepath.Join(p.Package.Root, "bin", name), r.Build.Arch); err != nil {
			return err
		}
	}
	targets := []struct {
		section, dest string
		uid           int
	}{{"data", r.DataDir, r.UID}, {"config", r.Paths.Config, r.OwnerUID}}
	if work != "" {
		targets = append(targets, struct {
			section, dest string
			uid           int
		}{"work", work, r.UID})
	}
	trees := []restoreTree{}
	defer func() {
		for _, t := range trees {
			if !t.moved {
				os.RemoveAll(filepath.Dir(t.staged))
			}
		}
	}()
	for _, target := range targets {
		if p.Previous == nil {
			if _, err = os.Lstat(target.dest); err == nil {
				return fmt.Errorf("new target path already exists: %s", target.dest)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		t, err := prepareTree(e.Ctx, filepath.Join(stage, target.section), target.dest, target.uid, r.GID)
		if err != nil {
			return err
		}
		if target.section == "config" && r.System {
			if err = filepath.WalkDir(t.staged, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				mode := os.FileMode(0640)
				if d.IsDir() {
					mode = 0750
				}
				return os.Chmod(p, mode)
			}); err != nil {
				os.RemoveAll(filepath.Dir(t.staged))
				return err
			}
		}
		trees = append(trees, t)
	}
	state, err := e.unitState(r)
	if err != nil {
		return err
	}
	wasRunning := running(state)
	if p.Previous == nil && state["LoadState"] != "not-found" {
		return fmt.Errorf("unmanaged service at target")
	}
	if err = e.stopService(r, state, false); err != nil {
		return err
	}
	e.UI.Stage = "Replace instance files"
	e.UI.State = "Target service stopped; recovery backup is prepared before replacement."
	rollbackArmed := false
	defer func() {
		if resultErr != nil && !rollbackArmed && p.Previous != nil && wasRunning {
			resultErr = errors.Join(resultErr, e.resumeBackup(p.Previous))
		}
	}()
	// Before replacing anything, preserve a complete recoverable target archive.
	if p.Previous != nil {
		lock, err := platform.NewLocker().Acquire(e.Ctx, r.DataDir)
		if err != nil {
			return err
		}
		backupStage, err := os.MkdirTemp("", "molr-pre-restore-")
		if err != nil {
			lock.Close()
			return err
		}
		defer os.RemoveAll(backupStage)
		old, err := instancebackup.Capture(e.Ctx, backupStage, *p.Previous)
		lock.Close()
		if err != nil {
			return err
		}
		if err = mkdirOwned(r.Paths.Cache, 0700, r.UID, r.GID); err != nil {
			return err
		}
		output := filepath.Join(r.Paths.Cache, "before-restore-"+time.Now().UTC().Format("20060102T150405.000000000")+".tar.gz")
		if err = instancebackup.Pack(e.Ctx, backupStage, output, old); err != nil {

			return err
		}
		e.UI.print("Previous instance backup: %s", output)
	}
	rollbackDir, files, err := snapshotInstallation(r)
	if err != nil {
		return err
	}
	keepRollback := false
	defer func() {
		if !keepRollback {
			os.RemoveAll(rollbackDir)
		}
	}()
	rollbackArmed = true
	committed := false
	defer func() {
		if resultErr == nil || committed {
			return
		}
		var recovery error
		for i := len(trees) - 1; i >= 0; i-- {
			t := &trees[i]
			if t.installed {
				recovery = errors.Join(recovery, os.RemoveAll(t.destination))
				t.installed = false
			}
			if t.moved {
				err := os.Rename(t.saved, t.destination)
				recovery = errors.Join(recovery, err)
				if err == nil {
					t.moved = false
				}
			}
		}
		recovery = errors.Join(recovery, restoreInstallation(r, files))
		if p.Previous != nil && wasRunning && recovery == nil {
			recovery = e.resumeBackup(p.Previous)
		}
		if recovery != nil {
			keepRollback = true
			e.UI.print("Recovery files retained: %s", rollbackDir)
		}
		resultErr = errors.Join(resultErr, recovery)
	}()
	if err = e.Ctx.Err(); err != nil {
		return err
	}
	for i := range trees {
		t := &trees[i]
		if _, err = os.Lstat(t.destination); err == nil {
			if err = os.Rename(t.destination, t.saved); err != nil {
				return err
			}
			t.moved = true
		} else if !os.IsNotExist(err) {
			return err
		}
		if err = os.Rename(t.staged, t.destination); err != nil {
			return err
		}
		t.installed = true
	}
	if err = mkdirOwned(filepath.Join(r.Paths.Program, "bin"), 0755, r.OwnerUID, r.GID); err != nil {
		return err
	}
	for _, name := range []string{"bin/molr", "bin/molr-setup", "release.json", "LICENSE", "THIRD_PARTY_NOTICES.md"} {
		mode := os.FileMode(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		if err = copyFile(filepath.Join(r.Paths.Program, name), filepath.Join(p.Package.Root, name), mode, r.OwnerUID, r.GID); err != nil {
			return err
		}
	}
	if err = mkdirOwned(r.Paths.Cache, 0700, r.UID, r.GID); err != nil {
		return err
	}
	if err = mkdirOwned(filepath.Join(r.Paths.Cache, "tmp"), 0700, r.UID, r.GID); err != nil {
		return err
	}
	r.State = "installed"
	r.Health = "not_started"
	if err = writeRecord(r); err != nil {
		return err
	}
	if err = mkdirOwned(filepath.Dir(r.Paths.Unit), 0755, r.OwnerUID, r.GID); err != nil {
		return err
	}
	if err = e.startService(r, delegate, false); err != nil {
		return err
	}
	committed = true
	e.UI.Stage = "Verify restored service"
	e.UI.State = "Data restored; the service may still require startup or authenticated verification."
	for i := range trees {
		os.RemoveAll(trees[i].saved)
		trees[i].moved = false
	}
	if !e.Options.NoStart {
		r.Health = "verification_pending"
		if err = writeRecord(r); err != nil {
			return err
		}
		if err = e.startService(r, delegate, true); err != nil {
			return fmt.Errorf("data restored; service failed to start: %w", err)
		}
		if _, err = e.verify(r, p.Config); err != nil {
			return fmt.Errorf("data restored; service verification failed: %w", err)
		}
		if err = writeRecord(r); err != nil {
			return err
		}
	}
	e.installCommands(r)
	e.UI.print("Restore complete: %s\nData: %s\nHealth: %s", r.Name, r.DataDir, r.Health)
	e.serviceInstructions(r)
	if r.Health == "verification_pending" {
		return &VerificationPending{}
	}
	return nil
}

// Estimate without reading file contents before stopping. Compression is not
// assumed: staged copies, the SQLite snapshot and archive may all coexist.
func backupSpace(r *installrecord.Record, cfg config.Config, stage, outputDir string) error {
	var size int64
	for _, root := range []string{r.DataDir, r.Paths.Config, r.Paths.Program, cfg.Sandbox.WorkRoot} {
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && p == filepath.Join(r.DataDir, "backups") {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() {
				st, err := d.Info()
				if err != nil {
					return err
				}
				size += st.Size()
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, root := range []string{stage, outputDir} {
		available := platform.FilesystemCapacity(root, 0)
		if available.StorageTotalBytes > 0 && available.StorageAvailableBytes < 3*size+(16<<20) {
			return fmt.Errorf("insufficient staging/archive space at %s (need approximately %d bytes)", root, 3*size+(16<<20))
		}
	}
	return nil
}
