//go:build linux || darwin

// Package installation implements package-local setup and installed-instance maintenance.
package installation

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/updates"
)

type Options struct {
	AutoInitAdmin                                                                                                                   bool
	Archive, Output                                                                                                                 string
	Overwrite, KeepStopped                                                                                                          bool
	Mode, Preset                                                                                                                    string
	Component, TaskID, TaskAction                                                                                                   string
	JSON                                                                                                                            bool
	LockFD                                                                                                                          int
	Command, InteractionMode, Name, User, ProgramRoot, Config, Record, Source, Channel, Version, PublicHost, Permissions, InitOwner string
	NameSet                                                                                                                         bool
	Port                                                                                                                            int
	NonInteractive, Interactive, DryRun, Yes, InstallDeps, SkipDependencies, NoStart, NoBrowser, Repair, Purge, AllowDowngrade      bool
}
type Engine struct {
	initPassword                []byte
	desktopLogin                string
	downloadBase, downloadTotal int64
	componentTransport          http.RoundTripper
	handoff                     func(context.Context, []string, *os.File) error
	lock                        *os.File
	task                        *Task
	taskRecord                  *installrecord.Record
	handedOff                   bool
	machineOut                  io.Writer
	Ctx                         context.Context
	Options                     Options
	UI                          *UI
	Input                       io.Reader
	Executable, Root            string
	Build                       buildinfo.Info
	Command                     func(context.Context, []string, io.Reader, []string, io.Writer, io.Writer) ([]byte, error)
}
type Package struct {
	Root  string
	Build installrecord.Build
}
type Plan struct {
	Record   *installrecord.Record
	Previous *installrecord.Record
	Package  *Package
	Config   map[string]any
	Choices  map[string]*permissionChoice
	Sandbox  bool
}

func parseOptions(args []string, diagnostics io.Writer) (Options, error) {
	o := Options{Command: "setup", Name: "molr", Permissions: "ask", InteractionMode: "auto"}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.Command = args[0]
		args = args[1:]
		if o.Command != "update" && o.Command != "uninstall" && o.Command != "status" && o.Command != "components" && o.Command != "tasks" && o.Command != "desktop" && o.Command != "configure" && o.Command != "backup" && o.Command != "restore" {
			return o, fmt.Errorf("unknown command %q; omit a subcommand to install the current package", o.Command)
		}
	}
	if o.Command == "components" || o.Command == "tasks" || o.Command == "desktop" {
		if len(args) == 0 {
			return o, fmt.Errorf("subcommand required")
		}
		o.TaskAction = args[0]
		args = args[1:]
		if o.Command != "desktop" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			if o.Command == "components" {
				o.Component = args[0]
			} else {
				o.TaskID = args[0]
			}
			args = args[1:]
		}
	}
	f := flag.NewFlagSet("molr-setup", flag.ContinueOnError)
	f.StringVar(&o.Archive, "archive", "", "instance backup archive to restore")
	f.StringVar(&o.Output, "output", "", "new backup archive path")
	f.BoolVar(&o.Overwrite, "overwrite", false, "replace the explicitly selected instance from backup")
	f.BoolVar(&o.KeepStopped, "keep-stopped", false, "leave the source stopped after backup")
	f.SetOutput(diagnostics)
	f.BoolVar(&o.JSON, "json", false, "machine-readable task output")
	f.StringVar(&o.TaskID, "task-id", o.TaskID, "maintenance task identity")
	f.IntVar(&o.LockFD, "lock-fd", 0, "internal verified installer handoff")
	f.BoolVar(&o.Interactive, "interactive", false, "require a terminal")
	f.BoolVar(&o.NonInteractive, "non-interactive", false, "never prompt or open a browser")
	f.BoolVar(&o.DryRun, "dry-run", false, "show the plan without installation changes")
	f.BoolVar(&o.Yes, "yes", false, "confirm the plan and requested system preparation; permissions have their own policy")
	f.StringVar(&o.Name, "name", "molr", "instance service name")
	f.StringVar(&o.User, "user", "", "system service account; requires root")
	f.StringVar(&o.ProgramRoot, "program-root", "", "program base directory; instance gets its own child")
	f.StringVar(&o.Mode, "mode", "", "Runtime mode: server (default) or sandbox; saved for maintenance")
	f.StringVar(&o.Preset, "resource-preset", "", "initial resource permissions: permissive (default) or least-privilege")
	f.StringVar(&o.Config, "config", "", "initial JSON configuration; existing configuration is preserved")
	f.StringVar(&o.Record, "record", "", "explicit installation.json to manage")
	f.StringVar(&o.Permissions, "directory-permissions", "ask", "ask, fix, or ignore writable directories")
	f.StringVar(&o.Source, "source", "", "release source base URL for future updates")
	f.StringVar(&o.Channel, "channel", "", "stable, preview, or nightly update subscription")
	f.StringVar(&o.Version, "version", "", "update to an explicit version (update command only)")
	f.IntVar(&o.Port, "port", 0, "initial port; default first free port from 8787")
	f.StringVar(&o.PublicHost, "public-host", "", "initial private IPv4 address")
	f.StringVar(&o.InitOwner, "init-owner", "", "initial administrator username; password from stdin")
	f.BoolVar(&o.AutoInitAdmin, "auto-init-admin", false, "generate owner credentials and save initial-password for unattended installation")
	f.BoolVar(&o.InstallDeps, "install-deps", false, "authorize system dependency and policy preparation")
	f.BoolVar(&o.SkipDependencies, "skip-dependencies", false, "prepare host dependencies and policy manually")
	f.BoolVar(&o.NoStart, "no-start", false, "prepare files without enabling or starting service")
	f.BoolVar(&o.NoBrowser, "no-browser", false, "print access instructions without opening a browser")
	f.BoolVar(&o.Repair, "repair", false, "reapply the current package, preserving configuration and data")
	f.BoolVar(&o.Purge, "purge", false, "uninstall and remove managed configuration and data")
	f.BoolVar(&o.AllowDowngrade, "allow-downgrade", false, "explicitly allow installing an older package")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 {
		return o, fmt.Errorf("unexpected arguments")
	}
	f.Visit(func(v *flag.Flag) {
		if v.Name == "name" {
			o.NameSet = true
		}
	})
	if (o.Command == "backup" && o.Output == "") || (o.Command == "restore" && o.Archive == "") {
		return o, fmt.Errorf("backup requires --output; restore requires --archive")
	}
	if (o.Output != "" || o.KeepStopped) && o.Command != "backup" {
		return o, fmt.Errorf("--output/--keep-stopped require backup")
	}
	if (o.Archive != "" || o.Overwrite) && o.Command != "restore" {
		return o, fmt.Errorf("--archive/--overwrite require restore")
	}
	if o.Mode != "" && o.Mode != "server" && o.Mode != "sandbox" {
		return o, fmt.Errorf("--mode must be server or sandbox")
	}
	if o.Preset != "" && o.Preset != "permissive" && o.Preset != "least-privilege" {
		return o, fmt.Errorf("invalid resource preset")
	}
	if o.Config != "" && o.Preset != "" {
		return o, fmt.Errorf("choose --config or --resource-preset")
	}
	if o.AutoInitAdmin && o.InitOwner != "" {
		return o, fmt.Errorf("choose --auto-init-admin or --init-owner")
	}
	if o.Mode == "sandbox" && (o.InitOwner != "" || o.AutoInitAdmin || o.Command == "desktop") {
		return o, fmt.Errorf("standalone sandbox does not initialize Runtime accounts")
	}
	if o.Interactive && o.NonInteractive {
		return o, fmt.Errorf("choose either --interactive or --non-interactive")
	}
	if o.Interactive {
		o.InteractionMode = "interactive"
	}
	if o.NonInteractive {
		o.InteractionMode = "non-interactive"
	}
	if o.Command == "desktop" {
		if !o.JSON || !o.NonInteractive || !o.NameSet || !strings.HasPrefix(o.Name, "molr-desktop-") || o.User != "" || o.Record != "" || o.Config != "" || o.AutoInitAdmin || (o.InitOwner != "" && o.TaskAction != "install") || o.Purge || o.AllowDowngrade || o.NoStart {
			return o, fmt.Errorf("desktop requires --json --non-interactive --name molr-desktop-<id> and a current-user managed instance")
		}
	}
	if o.Permissions != "ask" && o.Permissions != "fix" && o.Permissions != "ignore" {
		return o, fmt.Errorf("invalid directory permission policy")
	}
	if o.Purge && o.Command != "uninstall" {
		return o, fmt.Errorf("--purge is only valid for uninstall")
	}
	if o.Version != "" && o.Command != "update" {
		return o, fmt.Errorf("--version selects a download only with update; default setup installs this package")
	}
	if _, err := installrecord.Instance(o.Name); err != nil {
		return o, err
	}
	return o, nil
}
func Run(ctx context.Context, args []string, input io.Reader, out, diagnostics io.Writer) error {
	if len(args) > 0 && args[0] == "version" {
		if len(args) == 2 && args[1] == "--json" {
			return json.NewEncoder(out).Encode(buildinfo.Current())
		}
		fmt.Fprintln(out, "molr-setup", buildinfo.Version)
		return nil
	}
	o, err := parseOptions(args, diagnostics)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	humanOut := out
	if o.JSON {
		humanOut = diagnostics
	}
	ui, err := newUI(o.InteractionMode, o.DryRun, humanOut, diagnostics)
	if err != nil {
		return err
	}
	defer ui.close()
	ui.Yes = o.Yes
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	e := &Engine{Ctx: ctx, Options: o, UI: ui, Input: input, Executable: executable, Root: filepath.Dir(filepath.Dir(executable)), Build: buildinfo.Current(), Command: command}
	defer func() {
		if e.lock != nil {
			e.lock.Close()
		}
	}()
	if o.JSON {
		e.machineOut = out
	}
	err = e.execute()
	if finishErr := e.finishTask(err); err == nil {
		err = finishErr
	}
	if err != nil {
		fmt.Fprintf(diagnostics, "\nStopped during: %s\nState: %s\n", ui.Stage, ui.State)
	}
	return err
}
func (e *Engine) packageAt(root string) (*Package, error) {
	for _, name := range []string{"release.json", "LICENSE", "THIRD_PARTY_NOTICES.md", "bin/molr-setup"} {
		p := filepath.Join(root, name)
		if err := checkPath(p); err != nil {
			return nil, err
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("complete package required; missing %s", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "release.json"))
	if err != nil {
		return nil, err
	}
	var b installrecord.Build
	if err = json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	if b.Version != e.Build.Version || b.Commit != e.Build.Commit || b.OS != runtime.GOOS || b.Arch != runtime.GOARCH {
		return nil, fmt.Errorf("package identity does not match molr-setup")
	}
	for _, name := range []string{"molr-setup"} {
		if err = checkExecutable(filepath.Join(root, "bin", name), b.Arch); err != nil {
			return nil, err
		}
	}
	return &Package{Root: root, Build: b}, nil
}
func readRecord(path string, owner bool) (*installrecord.Record, error) {
	if err := checkPath(path); err != nil {
		return nil, err
	}
	r, err := installrecord.Read(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if int(info.Sys().(*syscall.Stat_t).Uid) != r.OwnerUID {
		return nil, fmt.Errorf("installation record owner mismatch")
	}
	if owner && r.OwnerUID != os.Geteuid() {
		return nil, fmt.Errorf("run this operation as installation owner UID %d", r.OwnerUID)
	}
	return r, nil
}
func (e *Engine) execute() error {
	if e.Options.Command == "backup" || e.Options.Command == "restore" {
		return e.backupCommand()
	}
	if e.Options.Command == "desktop" {
		return e.desktopCommand()
	}
	if e.Options.Command == "components" || e.Options.Command == "tasks" {
		return e.componentCommand()
	}
	installedPath := filepath.Join(e.Root, "installation.json")
	if _, err := os.Lstat(installedPath); err == nil {
		r, err := readRecord(installedPath, true)
		if err != nil {
			return err
		}
		if e.Options.Preset != "" && e.Options.Command != "configure" {
			e.UI.print("Existing configuration is preserved; use configure --resource-preset to explicitly change resource permissions.")
		}
		if e.Options.Mode != "" && e.Options.Mode != r.Mode {
			return fmt.Errorf("cannot change Runtime mode")
		}
		if e.Options.Record != "" && e.Options.Record != installedPath {
			return fmt.Errorf("installed tool manages its own instance; use that instance's tool")
		}
		if e.Options.NameSet && e.Options.Name != r.Name {
			return fmt.Errorf("--name does not match installed instance")
		}
		if e.Options.Command == "configure" {
			return e.configureResources(r)
		}
		if e.Options.Command == "uninstall" {
			return e.uninstall(r)
		}
		if e.Options.Command == "update" {
			if r.Manager == "desktop" {
				return fmt.Errorf("this Runtime is updated by Molr Desktop")
			}
			if e.Options.TaskID != "" {
				if err := e.beginTask(r, "update", "", e.Options.Version); err != nil {
					return err
				}
			}
			return e.update(r)
		}
		if e.Options.Command == "status" || (!e.Options.Repair && r.State == "installed") {
			return e.maintenance(r)
		}
		pkg, err := e.packageAt(e.Root)
		if err != nil {
			return fmt.Errorf("repair requires a complete package: %w", err)
		}
		return e.install(pkg, r)
	} else if !os.IsNotExist(err) {
		return err
	}
	var existing *installrecord.Record
	if e.Options.Record != "" {
		var err error
		existing, err = readRecord(e.Options.Record, true)
		if err != nil {
			return err
		}
	}
	if existing == nil && e.Options.Command != "setup" {
		r, err := e.locate()
		if err != nil {
			return err
		}
		existing = r
	}
	if existing != nil && e.Options.Mode != "" && e.Options.Mode != existing.Mode {
		return fmt.Errorf("cannot change Runtime mode")
	}
	switch e.Options.Command {
	case "configure":
		return e.configureResources(existing)
	case "uninstall":
		return e.uninstall(existing)
	case "update":
		return e.update(existing)
	case "status":
		return e.maintenance(existing)
	}
	pkg, err := e.packageAt(e.Root)
	if err != nil {
		return fmt.Errorf("not an installed tool or a complete package: %w", err)
	}
	return e.install(pkg, existing)
}
func (e *Engine) maintenance(r *installrecord.Record) error {
	e.UI.print("Instance: %s\nMode: %s\nVersion: %s\nState: %s\nProgram: %s\nUpdates: %s", r.Name, r.Mode, r.Build.Version, r.State, r.Paths.Program, r.UpdateSource())
	if e.Options.Command == "status" && !e.Options.DryRun {
		state, err := e.unitState(r)
		if err != nil {
			return err
		}
		e.UI.print("Service: %s (%s), PID: %s", state["ActiveState"], state["LoadState"], state["MainPID"])
	}
	if r.Mode == "sandbox" {
		cfg, err := desktopConfig(r)
		if err != nil {
			return err
		}
		if err = e.sandboxInstructions(r, cfg); err != nil {
			return err
		}
	}
	if e.Options.Command == "status" || e.Options.DryRun || !e.UI.interactive() {
		return nil
	}
	answer, err := e.UI.answer("[u] Check/update, [r] repair, [p] resource preset, [x] uninstall, [q] exit: ")
	if err != nil {
		return err
	}
	switch answer {
	case "p":
		return e.configureResources(r)
	case "u":
		return e.update(r)
	case "r":
		e.Options.Repair = true
		pkg, err := e.packageAt(e.Root)
		if err != nil {
			return fmt.Errorf("repair requires the complete package: %w", err)
		}
		return e.install(pkg, r)
	case "x":
		return e.uninstall(r)
	case "q", "":
		return nil
	default:
		return fmt.Errorf("unknown maintenance choice")
	}
}
func (e *Engine) locate() (*installrecord.Record, error) {
	a, err := currentAccount()
	if err != nil {
		return nil, err
	}
	p, _, err := layout(e.Options.Name, a.Home, e.Options.User != "", e.Options.ProgramRoot)
	if err != nil {
		return nil, err
	}
	return readRecord(filepath.Join(p.Program, "installation.json"), true)
}

var accountName = regexp.MustCompile(`^[a-z_][a-z0-9_.-]{0,31}$`)

func (e *Engine) plan(pkg *Package, previous *installrecord.Record) (*Plan, error) {
	o := e.Options
	if runtime.GOOS == "darwin" && (o.User != "" || os.Geteuid() == 0) {
		return nil, fmt.Errorf("macOS supports current-user installation; run without sudo or --user")
	}
	uid := os.Geteuid()
	var r installrecord.Record
	if previous != nil {
		if o.Command == "desktop" && previous.Manager != "desktop" {
			return nil, fmt.Errorf("cannot adopt an externally managed installation")
		}
		r = *previous
		if o.Mode != "" && o.Mode != r.Mode {
			return nil, fmt.Errorf("cannot change Runtime mode")
		}
		if o.Preset != "" {
			e.UI.print("Existing configuration is preserved; the initial resource preset is not reapplied.")
		}
		account, err := lookupAccount(r.User)
		if err != nil || account.UID != r.UID || account.GID != r.GID {
			return nil, fmt.Errorf("installed service account identity changed")
		}
		if o.NameSet && o.Name != r.Name {
			return nil, fmt.Errorf("instance name mismatch")
		}
		if o.User != "" && o.User != r.User {
			return nil, fmt.Errorf("cannot change installation account")
		}
		if o.ProgramRoot != "" && o.ProgramRoot != r.ProgramRoot {
			return nil, fmt.Errorf("cannot change program root")
		}
	} else {
		a, err := currentAccount()
		if err != nil {
			return nil, err
		}
		system := o.User != ""
		if system {
			if uid != 0 {
				return nil, fmt.Errorf("--user requires root")
			}
			if !accountName.MatchString(o.User) {
				return nil, fmt.Errorf("invalid service account")
			}
			a, err = lookupAccount(o.User)
			if err != nil {
				var missing user.UnknownUserError
				if !errors.As(err, &missing) {
					return nil, err
				}
				a = &Account{Name: o.User, Home: "/nonexistent", UID: -1, GID: -1}
			}
		} else if uid == 0 {
			return nil, fmt.Errorf("root installation requires --user USER; Runtime does not run as root")
		}
		if a.UID == 0 {
			return nil, fmt.Errorf("Runtime must not run as UID 0")
		}
		p, base, err := layout(o.Name, a.Home, system, o.ProgramRoot)
		if err != nil {
			return nil, err
		}
		recordPath := filepath.Join(p.Program, "installation.json")
		if _, err = os.Lstat(recordPath); err == nil {
			old, err := readRecord(recordPath, true)
			if err != nil {
				return nil, err
			}
			if old.User != a.Name || old.System != system || old.Paths != p {
				return nil, fmt.Errorf("existing installation uses different identity or paths")
			}
			return e.plan(pkg, old)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if _, err = os.Lstat(p.Unit); err == nil {
			return nil, fmt.Errorf("unmanaged service exists: %s", p.Unit)
		}
		r = installrecord.Record{Mode: o.Mode, Home: a.Home, Schema: 1, State: "installing", Name: o.Name, User: a.Name, UID: a.UID, GID: a.GID, OwnerUID: uid, System: system, Paths: p, ProgramRoot: base, DataDir: p.Data, ManagedData: true, Updates: installrecord.Updates{Source: "https://molr.ai/releases", Channel: channelFor(pkg.Build.Version)}}
		if o.Command == "desktop" {
			r.Manager = "desktop"
		}
	}
	if r.OwnerUID != uid {
		return nil, fmt.Errorf("installation owner mismatch")
	}
	if o.Source != "" {
		r.Updates.Source = strings.TrimRight(o.Source, "/")
	}
	if o.Channel != "" {
		r.Updates.Channel = o.Channel
	}
	if err := validateSubscription(r.Updates); err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(r.Paths.Config, "server.json")
	cfg := map[string]any{}
	source := cfgPath
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		source = o.Config
	}

	if source != "" {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &cfg); err != nil || cfg == nil {
			return nil, fmt.Errorf("configuration must be a JSON object")
		}
	}
	if v, ok := cfg["data_dir"]; ok {
		p, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("data_dir must be a path")
		}
		r.DataDir = p
		r.ManagedData = p == r.Paths.Data
	}
	cfg["data_dir"] = r.DataDir
	if source == "" && previous == nil {
		if r.Mode == "" {
			r.Mode = "server"
			if e.UI.interactive() && o.Command != "desktop" && o.Command != "configure" {
				answer, err := e.UI.answer("Runtime mode: [r] server / [s] sandbox node [r]: ")
				if err != nil {
					return nil, err
				}
				if answer == "s" {
					r.Mode = "sandbox"
				} else if answer != "" && answer != "r" {
					return nil, fmt.Errorf("invalid Runtime mode")
				}
			}
		}
		preset := o.Preset
		if preset == "" && e.UI.interactive() {
			e.UI.print("Personal (permissive, default): allow connecting local directories, networks and compute within the service account's OS permissions. Assistants still require grants.\nLeast privilege: expose no external directories, networks or command execution; enable resources later in deployment configuration.")
			answer, err := e.UI.answer("Resource preset: [p] personal / [l] least privilege [p]: ")
			if err != nil {
				return nil, err
			}
			if answer == "l" {
				preset = "least-privilege"
			} else if answer != "" && answer != "p" {
				return nil, fmt.Errorf("invalid resource preset")
			}
		}
		if preset == "" {
			preset = "permissive"
		}
		initial, err := config.SandboxPreset(preset)
		if err != nil {
			return nil, err
		}
		var value map[string]any
		if err = json.Unmarshal(jsonBytes(initial), &value); err != nil {
			return nil, err
		}
		cfg["sandbox"] = value
	}
	if r.Mode == "" {
		r.Mode = "server"
	}
	sandbox, err := object(cfg, "sandbox")
	if err != nil {
		return nil, err
	}
	if _, ok := sandbox["host_tools"]; !ok {
		sandbox["host_tools"] = sandbox["rootfs"] == nil || sandbox["rootfs"] == ""
	}
	if runtime.GOOS == "darwin" {
		if _, ok := sandbox["enabled"]; !ok {
			sandbox["enabled"] = false
		}
	}
	enabled := true
	if v, ok := sandbox["enabled"]; ok {
		var good bool
		enabled, good = v.(bool)
		if !good {
			return nil, fmt.Errorf("sandbox.enabled must be boolean")
		}
	}
	if source == "" {
		http, err := defaultHTTP(o.Port, o.PublicHost)
		if err != nil {
			return nil, err
		}
		cfg["http"] = http
		if o.Command == "desktop" {
			address := fmt.Sprintf("127.0.0.1:%s", strings.TrimPrefix(http["listen"].(string), "0.0.0.0:"))
			cfg["http"] = map[string]any{"listen": address, "public_url": "http://" + address, "tls_mode": "local"}
		}
	}
	if r.Mode == "sandbox" && source == "" {
		h := cfg["http"].(map[string]any)
		h["tls_mode"] = "direct"
		delete(h, "allowed_origins")
		h["public_url"] = strings.Replace(h["public_url"].(string), "http://", "https://", 1)
		h["auto_renew_certificate"] = true
		h["cert_file"] = filepath.Join(r.DataDir, "sandbox.crt")
		h["key_file"] = filepath.Join(r.DataDir, "sandbox.key")
	}
	if _, err := config.Load(strings.NewReader(string(jsonBytes(cfg)))); err != nil {
		return nil, err
	}
	if r.Mode == "sandbox" {
		h, ok := cfg["http"].(map[string]any)
		if !ok || h["tls_mode"] != "direct" {
			return nil, fmt.Errorf("standalone sandbox requires direct TLS")
		}
		key, _ := h["key_file"].(string)
		rel, err := filepath.Rel(r.DataDir, key)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("sandbox TLS key must be inside data_dir")
		}
	}
	if err = checkPath(r.DataDir); err != nil {
		return nil, err
	}
	if err = separate(r.Paths.Program, r.Paths.Config, r.Paths.Cache, r.DataDir, r.Paths.Unit); err != nil {
		return nil, err
	}
	choices := map[string]*permissionChoice{}
	for _, p := range []string{r.Paths.Program, r.Paths.Config, r.Paths.Unit, filepath.Join(r.Paths.Cache, ".."), r.File()} {
		p = filepath.Clean(p)
		if err = checkManaged(p, uid, choices); err != nil {
			return nil, err
		}
	}
	for _, relative := range programFiles {
		if err = checkManaged(filepath.Join(r.Paths.Program, relative), uid, choices); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"server.json", "server.previous.json"} {
		if err = checkManaged(filepath.Join(r.Paths.Config, name), uid, choices); err != nil {
			return nil, err
		}
	}
	for _, p := range []string{r.DataDir, r.Paths.Cache, filepath.Join(r.Paths.Cache, "tmp")} {
		if err = checkPath(p); err != nil {
			return nil, err
		}
		if info, err := os.Stat(p); err == nil {
			if !info.IsDir() || int(info.Sys().(*syscall.Stat_t).Uid) != r.UID {
				return nil, fmt.Errorf("data/cache directory must belong to service account: %s", p)
			}
			if info.Mode().Perm()&0022 != 0 {
				choices[p] = &permissionChoice{path: p, info: info}
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	r.Build = pkg.Build
	return &Plan{Record: &r, Previous: previous, Package: pkg, Config: cfg, Choices: choices, Sandbox: enabled}, nil
}
func object(v map[string]any, key string) (map[string]any, error) {
	if x, ok := v[key]; ok {
		m, ok := x.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		return m, nil
	}
	m := map[string]any{}
	v[key] = m
	return m, nil
}
func (e *Engine) showPlan(p *Plan) {
	r := p.Record
	e.UI.print("Runtime mode: %s", r.Mode)
	e.UI.print("System: %s / %s\nInstance: %s\nAccount: %s\nPackage: %s\nProgram: %s\nConfiguration: %s\nData: %s\nUpdates: %s", hostLabel(), runtime.GOARCH, r.Name, r.User, r.Build.Version, r.Paths.Program, r.Paths.Config, r.DataDir, r.UpdateSource())
	if p.Previous != nil {
		e.UI.print("Installed version: %s (%s)", p.Previous.Build.Version, p.Previous.State)
	}
	if p.Sandbox {
		e.UI.print("Missing dependencies: %s", strings.Join(missingDependencies(), ", "))
	}
	for _, c := range sortedChoices(p.Choices) {
		e.UI.print("Directory is group/other writable: %s (%04o); policy=%s", c.path, c.info.Mode().Perm(), e.Options.Permissions)
	}
}
func sortedChoices(m map[string]*permissionChoice) []*permissionChoice {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []*permissionChoice{}
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
func (e *Engine) resolvePermissions(choices map[string]*permissionChoice) error {
	for _, c := range sortedChoices(choices) {
		c.action = e.Options.Permissions
		if c.action == "ask" {
			e.UI.print("Remove only group/other write bits on %s, keep permissions, or cancel.", c.path)
			action, err := e.UI.directory(c.path)
			if err != nil {
				return err
			}
			c.action = action
		}
	}
	return nil
}
func (e *Engine) install(pkg *Package, existing *installrecord.Record) error {
	e.UI.step(1, "Inspect package and installation plan")
	p, err := e.plan(pkg, existing)
	if err != nil {
		return err
	}
	e.showPlan(p)
	if p.Previous != nil && p.Previous.State == "installed" && !e.Options.Repair {
		n, ok := updates.Compare(pkg.Build.Version, p.Previous.Build.Version)
		if !ok {
			return fmt.Errorf("cannot compare installed and package versions")
		}
		if n == 0 && pkg.Build.Commit == p.Previous.Build.Commit && !e.Options.DryRun && e.Options.Command != "desktop" {
			return e.maintenance(p.Previous)
		}
		if n < 0 && !e.Options.AllowDowngrade {
			return fmt.Errorf("package is older; use --allow-downgrade only for an intended downgrade")
		}
	}
	if e.Options.DryRun {
		e.UI.print("Dry run complete: no installation changes; runtime readiness has not been verified.")
		return nil
	}
	if err = e.resolvePermissions(p.Choices); err != nil {
		return err
	}
	ok, err := e.UI.confirm("Apply this installation plan?")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("installation cancelled; no changes made")
	}
	if err = e.prepareAdministrator(p); err != nil {
		return err
	}
	defer func() { clear(e.initPassword); e.initPassword = nil }()
	return e.apply(p)
}
