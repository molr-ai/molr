package installationwindows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/instancebackup"
	"molr.ai/runtime/internal/platform"
)

func backupRecord(root, name string) (record, error) {
	raw, err := os.ReadFile(filepath.Join(root, "installation.json"))
	if err != nil {
		return record{}, err
	}
	var r record
	if err = json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	if r.Manager != "desktop" || r.Name != name || r.DataDir != filepath.Join(root, "data") || r.Paths.Unit != filepath.Join(root, "task.xml") || !strings.HasPrefix(strings.ToLower(r.Binary), strings.ToLower(filepath.Join(root, "versions")+string(filepath.Separator))) {
		return r, fmt.Errorf("installation ownership mismatch")
	}
	return r, nil
}
func backupPackage(ctx context.Context, r record, root string) (string, error) {
	// New installations retain the complete package beside the Runtime. This
	// also permits preserving an older target before replacement by a backup.
	pkg := filepath.Dir(filepath.Dir(r.Binary))
	raw, err := os.ReadFile(filepath.Join(pkg, "release.json"))
	if err != nil {
		return "", fmt.Errorf("complete installed package required for backup: %w", err)
	}
	var build installrecord.Build
	if err = json.Unmarshal(raw, &build); err != nil {
		return "", err
	}
	actualRaw, err := command(ctx, r.Binary, "version", "--json")
	if err != nil {
		return "", err
	}
	var actual installrecord.Build
	if err = json.Unmarshal(actualRaw, &actual); err != nil {
		return "", err
	}
	if actual != build {
		return "", fmt.Errorf("installed Runtime does not match package identity")
	}
	if err = instancebackup.Add(ctx, root, pkg, "program", nil); err != nil {
		return "", err
	}
	return filepath.Join(root, "program"), nil
}
func captureWindows(ctx context.Context, r record, root, stage string) (*instancebackup.Manifest, error) {
	source, err := os.MkdirTemp("", "molr-backup-source-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(source)
	pkg, err := backupPackage(ctx, r, source)
	if err != nil {
		return nil, err
	}
	var build installrecord.Build
	raw, err := os.ReadFile(filepath.Join(pkg, "release.json"))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &build); err != nil {
		return nil, err
	}
	configDir := filepath.Join(source, "config")
	if err = os.Mkdir(configDir, 0700); err != nil {
		return nil, err
	}
	if err = copyFile(filepath.Join(root, "server.json"), filepath.Join(configDir, "server.json")); err != nil {
		return nil, err
	}
	return instancebackup.Capture(ctx, stage, installrecord.Record{Mode: "server", DataDir: r.DataDir, Paths: installrecord.Paths{Program: pkg, Config: configDir}, Build: build})
}
func runBackup(ctx context.Context, args []string, out, diagnostics io.Writer) (resultErr error) {
	action := args[0]
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	name := flags.String("name", "", "installed desktop instance name")
	rootArg := flags.String("program-root", "", "private installation root")
	output := flags.String("output", "", "new backup archive")
	archive := flags.String("archive", "", "backup archive to restore")
	overwrite := flags.Bool("overwrite", false, "replace selected instance")
	keep := flags.Bool("keep-stopped", false, "leave backup source stopped")
	noStart := flags.Bool("no-start", false, "restore without starting")
	dry := flags.Bool("dry-run", false, "validate and show plan")
	yes := flags.Bool("yes", false, "confirm restore")
	flags.Bool("non-interactive", false, "")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || !filepath.IsAbs(*rootArg) {
		return fmt.Errorf("absolute --program-root required")
	}
	if _, err := installrecord.Instance(*name); err != nil {
		return err
	}
	root := filepath.Clean(*rootArg)
	for p := root; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("installation path must not contain reparse links")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	r, err := backupRecord(root, *name)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if action == "backup" {
		if !exists || !r.Installed || *output == "" {
			return fmt.Errorf("installed instance and --output required")
		}
		dest, err := filepath.Abs(*output)
		if err != nil {
			return err
		}
		if strings.HasPrefix(strings.ToLower(dest), strings.ToLower(root+string(filepath.Separator))) {
			return fmt.Errorf("archive must be outside installation")
		}
		if _, err = os.Lstat(dest); !os.IsNotExist(err) {
			return fmt.Errorf("archive destination already exists or is inaccessible")
		}
		if err = verifyTask(ctx, r, filepath.Join(root, "server.json")); err != nil {
			return err
		}
		if *dry {
			fmt.Fprintln(out, "Backup:", r.Name, "to", dest)
			return nil
		}
		lock, err := platform.NewLocker().Acquire(ctx, root)
		if err != nil {
			return err
		}
		defer lock.Close()
		active := platform.ServiceRunning(r.DataDir)
		if err = stop(ctx, r); err != nil {
			return err
		}
		resume := active
		defer func() {
			if resume {
				c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
				defer cancel()
				_, e := scheduler(c, "/Run", "/TN", r.Name)
				resultErr = errors.Join(resultErr, e)
			}
		}()
		dataLock, err := platform.NewLocker().Acquire(ctx, r.DataDir)
		if err != nil {
			return err
		}
		stage, err := os.MkdirTemp("", "molr-backup-")
		if err != nil {
			dataLock.Close()
			return err
		}
		defer os.RemoveAll(stage)
		m, err := captureWindows(ctx, r, root, stage)
		dataLock.Close()
		if err != nil {
			return err
		}
		if active && !*keep {
			if _, err = scheduler(ctx, "/Run", "/TN", r.Name); err != nil {
				return err
			}
			resume = false
		}
		if err = instancebackup.Pack(ctx, stage, dest, m); err != nil {
			return err
		}
		resume = false
		fmt.Fprintln(out, "Backup complete:", dest)
		return nil
	}
	if *archive == "" {
		return fmt.Errorf("--archive required")
	}
	stage, m, err := instancebackup.Unpack(ctx, *archive, "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if m.Mode != "server" {
		return fmt.Errorf("Windows setup currently manages Runtime desktop instances only")
	}
	if err = instancebackup.CheckDatabase(ctx, stage, m); err != nil {
		return err
	}
	current := buildinfo.Current()
	if current.Version != m.Build.Version || current.Commit != m.Build.Commit {
		return fmt.Errorf("use target-platform molr-setup %s (%s)", m.Build.Version, m.Build.Commit)
	}
	if exists && !*overwrite {
		return fmt.Errorf("instance exists; use --overwrite or a new name and root")
	}
	if !exists {
		if _, err = os.Lstat(root); !os.IsNotExist(err) {
			return fmt.Errorf("new installation root must not exist")
		}
		if _, err = scheduler(ctx, "/Query", "/TN", *name, "/XML"); err == nil {
			return fmt.Errorf("scheduled task already exists")
		}
	}
	if exists {
		if err = verifyTask(ctx, r, filepath.Join(root, "server.json")); err != nil {
			return err
		}
	}
	pkg := filepath.Join(stage, "program")
	if m.Build.OS != "windows" || m.Build.Arch != runtime.GOARCH {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		pkg = filepath.Dir(filepath.Dir(exe))
		fmt.Fprintln(out, "Ignoring source-platform binaries; using target-platform package.")
	}
	raw, err := os.ReadFile(filepath.Join(pkg, "release.json"))
	if err != nil {
		return err
	}
	var b installrecord.Build
	if err = json.Unmarshal(raw, &b); err != nil {
		return err
	}
	if b.Version != m.Build.Version || b.Commit != m.Build.Commit || b.OS != "windows" || b.Arch != runtime.GOARCH {
		return fmt.Errorf("target package identity mismatch")
	}
	work := ""
	if m.WorkRoot != "" {
		work = filepath.Join(root, "work")
	}
	cfg, notes, err := instancebackup.Configuration(m, filepath.Join(root, "data"), filepath.Join(root, "config"), work)
	if err != nil {
		return err
	}
	if err = instancebackup.RelocateLinks(stage, m, filepath.Join(root, "data"), work); err != nil {
		return err
	}
	for _, note := range notes {
		fmt.Fprintln(out, note)
	}
	// Windows desktop registration always uses a local HTTP endpoint.
	parsed, err := config.Load(bytes.NewReader(jsonData(cfg)))
	if err != nil {
		return err
	}
	if parsed.HTTP.TLSMode != config.LocalHTTP {
		fmt.Fprintln(out, "Desktop restore uses loopback HTTP on port 8787; edit server.json before starting to change it.")
		cfg["http"] = map[string]any{"listen": "127.0.0.1:8787", "public_url": "http://127.0.0.1:8787", "tls_mode": "local"}
	}
	if *dry {
		fmt.Fprintln(out, "Restore:", *name, "to", root, "overwrite:", exists)
		return nil
	}
	if !*yes {
		return fmt.Errorf("--yes required to apply restore")
	}
	return restoreWindows(ctx, root, *name, r, exists, stage, m, pkg, cfg, work, *noStart, out)
}
func jsonData(v any) []byte { b, _ := json.Marshal(v); return b }
func restoreWindows(ctx context.Context, root, name string, previous record, exists bool, stage string, m *instancebackup.Manifest, pkg string, cfg map[string]any, work string, noStart bool, out io.Writer) (resultErr error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if err := platform.PrivateDirectory(root); err != nil {
		return err
	}
	lock, err := platform.NewLocker().Acquire(ctx, root)
	if err != nil {
		return err
	}
	defer lock.Close()
	active := exists && platform.ServiceRunning(previous.DataDir)
	if exists {
		if err = stop(ctx, previous); err != nil {
			return err
		}
	}
	committed := false
	defer func() {
		if resultErr != nil && !committed && active {
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
			defer cancel()
			_, e := scheduler(c, "/Run", "/TN", previous.Name)
			resultErr = errors.Join(resultErr, e)
		}
	}()
	if exists {
		dataLock, err := platform.NewLocker().Acquire(ctx, previous.DataDir)
		if err != nil {
			return err
		}
		saved, err := os.MkdirTemp("", "molr-before-restore-")
		if err != nil {
			dataLock.Close()
			return err
		}
		defer os.RemoveAll(saved)
		old, err := captureWindows(ctx, previous, root, saved)
		dataLock.Close()
		if err != nil {
			return err
		}
		dest := filepath.Join(filepath.Dir(root), name+"-before-restore-"+time.Now().Format("20060102T150405.000000000")+".tar.gz")
		if err = instancebackup.Pack(ctx, saved, dest, old); err != nil {
			return err
		}
		fmt.Fprintln(out, "Previous instance backup:", dest)
	}
	pending, err := os.MkdirTemp(root, ".restore-")
	if err != nil {
		return err
	}
	keepPending := false
	defer func() {
		if !keepPending {
			os.RemoveAll(pending)
		}
	}()
	for _, section := range []string{"data", "config"} {
		if err = instancebackup.Add(ctx, pending, filepath.Join(stage, section), section, nil); err != nil {
			return err
		}
	}
	if work != "" {
		if err = instancebackup.Add(ctx, pending, filepath.Join(stage, "work"), "work", nil); err != nil {
			return err
		}
	}
	if err = instancebackup.Add(ctx, pending, pkg, "package", nil); err != nil {
		return err
	}
	versionDir := filepath.Join(root, "versions", m.Build.Version+"-restore-"+time.Now().Format("20060102T150405.000000000"))
	if err = os.MkdirAll(filepath.Dir(versionDir), 0700); err != nil {
		return err
	}
	if err = os.Rename(filepath.Join(pending, "package"), versionDir); err != nil {
		return err
	}
	r := record{Manager: "desktop", Name: name, Installed: true, DataDir: filepath.Join(root, "data"), Version: m.Build.Version, Binary: filepath.Join(versionDir, "bin", "molr.exe")}
	r.Paths.Unit = filepath.Join(root, "task.xml")
	httpCfg, _ := cfg["http"].(map[string]any)
	r.URL, _ = httpCfg["public_url"].(string)
	sections := []string{"data", "config"}
	if work != "" {
		sections = append(sections, "work")
	}
	moved, installed := []string{}, []string{}
	registered := false
	recordRaw, _ := os.ReadFile(filepath.Join(root, "installation.json"))
	configRaw, _ := os.ReadFile(filepath.Join(root, "server.json"))
	taskRaw, _ := os.ReadFile(r.Paths.Unit)
	defer func() {
		if resultErr == nil || committed {
			return
		}
		var recovery error
		for _, section := range installed {
			recovery = errors.Join(recovery, os.RemoveAll(filepath.Join(root, section)))
		}
		for _, section := range moved {
			recovery = errors.Join(recovery, os.Rename(filepath.Join(pending, "old-"+section), filepath.Join(root, section)))
		}
		for file, body := range map[string][]byte{"installation.json": recordRaw, "server.json": configRaw, "task.xml": taskRaw} {
			if body != nil {
				recovery = errors.Join(recovery, os.WriteFile(filepath.Join(root, file), body, 0600))
			} else {
				os.Remove(filepath.Join(root, file))
			}
		}
		if exists && len(taskRaw) > 0 {
			_, err := scheduler(ctx, "/Create", "/TN", previous.Name, "/XML", previous.Paths.Unit, "/F")
			recovery = errors.Join(recovery, err)
		}
		if !exists && registered {
			_, err := scheduler(context.WithoutCancel(ctx), "/Delete", "/TN", name, "/F")
			recovery = errors.Join(recovery, err)
		}
		if recovery != nil {
			keepPending = true
			fmt.Fprintln(out, "Recovery files retained:", pending)
			active = false
		}
		resultErr = errors.Join(resultErr, recovery)
	}()
	for _, section := range sections {
		dest := filepath.Join(root, section)
		if _, err = os.Stat(dest); err == nil {
			if err = os.Rename(dest, filepath.Join(pending, "old-"+section)); err != nil {
				return err
			}
			moved = append(moved, section)
		}
		if err = os.Rename(filepath.Join(pending, section), dest); err != nil {
			return err
		}
		installed = append(installed, section)
	}
	if err = writeJSON(filepath.Join(root, "server.json"), cfg); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(root, "config", "restore-source.json"), m); err != nil {
		return err
	}
	sid, err := currentSID()
	if err != nil {
		return err
	}
	if err = os.WriteFile(r.Paths.Unit, taskXMLFile(taskXML(r, filepath.Join(root, "server.json"), sid)), 0600); err != nil {
		return err
	}
	if _, err = scheduler(ctx, "/Create", "/TN", name, "/XML", r.Paths.Unit, "/F"); err != nil {
		return err
	}
	registered = true
	if err = writeJSON(filepath.Join(root, "installation.json"), r); err != nil {
		return err
	}
	committed = true
	if !noStart {
		if _, err = scheduler(ctx, "/Run", "/TN", name); err != nil {
			return err
		}
		if err = waitReady(ctx, r); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Data restored:", root, "; authenticated sandbox verification is pending.")
	return nil
}
