package installation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"molr.ai/runtime/internal/installrecord"
)

var programFiles = []string{"bin/molr", "bin/molr-setup", "bin/molr.previous", "bin/molr-setup.previous", "LICENSE", "THIRD_PARTY_NOTICES.md", "release.json"}

func writeRecord(r *installrecord.Record) error {
	mode := os.FileMode(0600)
	if r.System {
		mode = 0640
	}
	return atomicWrite(r.File(), bytes.NewReader(jsonBytes(r)), mode, r.OwnerUID, r.GID)
}
func mkdirOwned(path string, mode os.FileMode, uid, gid int) error {
	d, err := openDir(path, true)
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Chmod(mode); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return d.Chown(uid, gid)
	}
	return nil
}
func (e *Engine) apply(p *Plan) error {
	r := p.Record
	if err := e.acquire(r); err != nil {
		return err
	}
	if r.UID >= 0 {
		op := "install"
		if e.Options.LockFD == 3 {
			op = "update"
		}
		if err := e.beginTask(r, op, "", r.Build.Version); err != nil {
			return err
		}
	}
	e.UI.step(2, "Prepare permissions and system dependencies")
	e.UI.State = "Permissions or host dependencies may have changed; the existing service has not been stopped."
	for _, choice := range sortedChoices(p.Choices) {
		if err := applyPermission(choice); err != nil {
			return err
		}
	}
	var err error
	if p.Previous != nil {
		current, err := readRecord(r.File(), true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, p.Previous) {
			return fmt.Errorf("installation changed since planning; rerun the command")
		}
	} else if _, err = os.Lstat(r.File()); err == nil {
		return fmt.Errorf("another installation completed since planning")
	}
	if err = e.prepareDependencies(p.Sandbox); err != nil {
		return err
	}
	delegate, err := e.checkServiceHost(r, p.Sandbox)
	if err != nil {
		return err
	}
	if r.UID < 0 {
		if _, err = e.run([]string{"useradd", "--system", "--user-group", "--no-create-home", "--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", r.User}, false); err != nil {
			return err
		}
		a, err := lookupAccount(r.User)
		if err != nil {
			return err
		}
		r.UID, r.GID = a.UID, a.GID
	}
	if r.System && e.lock != nil {
		if err = e.lock.Chown(r.OwnerUID, r.GID); err != nil {
			return err
		}
		if err = e.lock.Chmod(0640); err != nil {
			return err
		}
	}
	if p.Sandbox {
		if err = e.preparePolicy(r); err != nil {
			return err
		}
	}
	// Check access and both executable identities as the actual service account before replacing anything.
	for _, name := range []string{"molr", "molr-setup"} {
		b, err := e.run(asUser(r, filepath.Join(p.Package.Root, "bin", name), "version", "--json"), true)
		if err != nil {
			return err
		}
		var actual installrecord.Build
		if err = json.Unmarshal(b, &actual); err != nil {
			return err
		}
		if actual != p.Package.Build {
			return fmt.Errorf("%s build identity does not match package", name)
		}
	}
	if err = r.Validate(); err != nil {
		return err
	}
	if err = e.progress("installing", 0, 0); err != nil {
		return err
	}
	e.UI.step(3, "Install programs, configuration and service")
	e.UI.State = "Instance files may have changed. The installation record identifies the recoverable state."
	for _, path := range []string{filepath.Join(r.Paths.Program, "bin"), r.Paths.Config, filepath.Dir(r.Paths.Unit)} {
		d, err := openDir(path, true)
		if err != nil {
			return err
		}
		d.Close()
	}
	for _, path := range []string{r.DataDir, r.Paths.Cache, filepath.Join(r.Paths.Cache, "tmp")} {
		if _, err = os.Stat(path); os.IsNotExist(err) {
			if err = mkdirOwned(path, 0700, r.UID, r.GID); err != nil {
				return err
			}
		}
	}
	if p.Previous != nil {
		state, err := e.unitState(r)
		if err != nil {
			return err
		}
		if state["LoadState"] != "not-found" {
			if err = e.service(r, "stop", r.Name+".service"); err != nil {
				return err
			}
		}
	}
	r.State = "installing"
	r.Health = "verification_pending"
	if err = writeRecord(r); err != nil {
		return err
	}
	configPath := filepath.Join(r.Paths.Config, "server.json")
	mode := os.FileMode(0600)
	if r.System {
		mode = 0640
	}
	if _, err = os.Stat(configPath); err == nil {
		if err = copyFile(filepath.Join(r.Paths.Config, "server.previous.json"), configPath, mode, r.OwnerUID, r.GID); err != nil {
			return err
		}
	}
	if err = atomicWrite(configPath, bytes.NewReader(jsonBytes(p.Config)), mode, r.OwnerUID, r.GID); err != nil {
		return err
	}
	for _, name := range []string{"molr", "molr-setup"} {
		destination := filepath.Join(r.Paths.Program, "bin", name)
		if _, err = os.Stat(destination); err == nil {
			if err = copyFile(destination+".previous", destination, 0755, r.OwnerUID, r.GID); err != nil {
				return err
			}
		}
		if err = copyFile(destination, filepath.Join(p.Package.Root, "bin", name), 0755, r.OwnerUID, r.GID); err != nil {
			return err
		}
	}
	for _, name := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md", "release.json"} {
		if err = copyFile(filepath.Join(r.Paths.Program, name), filepath.Join(p.Package.Root, name), 0644, r.OwnerUID, r.GID); err != nil {
			return err
		}
	}
	runtime := filepath.Join(r.Paths.Program, "bin/molr")
	_, databaseErr := os.Stat(filepath.Join(r.DataDir, "runtime.db"))
	if e.Options.InitOwner != "" {
		if _, err = e.Command(e.Ctx, asUser(r, runtime, "init", "-config", configPath, "-username", e.Options.InitOwner, "-password-stdin"), e.Input, nil, e.UI.Out, e.UI.Err); err != nil {
			return err
		}
	} else if os.IsNotExist(databaseErr) || p.Previous == nil || p.Previous.State == "installing" {
		if e.Options.Config == "" {
			if _, err = e.run(asUser(r, runtime, "bootstrap", "-config", configPath), false); err != nil {
				return err
			}
		}
	}
	if err = atomicWrite(r.Paths.Unit, strings.NewReader(renderUnit(r, delegate)), 0644, r.OwnerUID, r.GID); err != nil {
		return err
	}
	r.State = "installed"
	if err = writeRecord(r); err != nil {
		return err
	}
	if err = e.progress("restarting", 0, 0); err != nil {
		return err
	}
	e.UI.step(4, "Start service and verify health")
	if err = e.service(r, "daemon-reload"); err != nil {
		return err
	}
	var link string
	if e.Options.NoStart {
		r.Health = "not_started"
	} else {
		if err = e.service(r, "enable", r.Paths.Unit); err != nil {
			return err
		}
		if err = e.service(r, "restart", r.Name+".service"); err != nil {
			return err
		}
		link, err = e.verify(r, p.Config)
		if err != nil {
			return err
		}
	}
	if err = writeRecord(r); err != nil {
		return err
	}
	e.UI.step(5, "Result and next steps")
	e.UI.print("Result: %s\nInstance: %s\nVersion: %s\nProgram: %s\nConfiguration: %s\nData: %s", r.Health, r.Name, r.Build.Version, r.Paths.Program, configPath, r.DataDir)
	e.UI.print("Service status: %s status %s.service\nService logs: journalctl %s-u %s.service -n 50 --no-pager", shellJoin(control(r)), r.Name, map[bool]string{false: "--user "}[r.System], r.Name)
	e.UI.print("Update: %s update\nUninstall: %s uninstall", shellJoin([]string{filepath.Join(r.Paths.Program, "bin/molr-setup")}), shellJoin([]string{filepath.Join(r.Paths.Program, "bin/molr-setup")}))
	if !r.System {
		e.UI.print("For operation after logout: sudo loginctl enable-linger %s", r.User)
	}
	if e.Options.NoStart {
		e.UI.print("Start after initialization: %s enable --now %s", shellJoin(control(r)), shellJoin([]string{r.Paths.Unit}))
	}
	if link != "" {
		e.UI.print("One-time login link (15 minutes):\n%s", link)
		e.openBrowser(link, r.System)
	}
	if r.Health == "verification_pending" {
		e.UI.print("Verify with current credentials: %s doctor -config %s -password-stdin", shellJoin(asUser(r, runtime)), shellJoin([]string{configPath}))
		return &VerificationPending{}
	}
	return nil
}
func shellJoin(args []string) string {
	out := []string{}
	for _, a := range args {
		out = append(out, "'"+strings.ReplaceAll(a, "'", "'\\''")+"'")
	}
	return strings.Join(out, " ")
}

type VerificationPending struct{}

func (*VerificationPending) Error() string {
	return "program installed; authenticated verification is pending"
}
