// Package installation implements package-local setup and installed-instance maintenance.
package installation

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/updates"
)

type Options struct {
	Command, Mode, Name, User, ProgramRoot, Config, Record, Source, Channel, Version, PublicHost, Permissions, InitOwner       string
	NameSet                                                                                                                    bool
	Port                                                                                                                       int
	NonInteractive, Interactive, DryRun, Yes, InstallDeps, SkipDependencies, NoStart, NoBrowser, Repair, Purge, AllowDowngrade bool
}
type Engine struct {
	Ctx              context.Context
	Options          Options
	UI               *UI
	Input            io.Reader
	Executable, Root string
	Build            buildinfo.Info
	Command          func(context.Context, []string, io.Reader, []string, io.Writer, io.Writer) ([]byte, error)
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
	o := Options{Command: "setup", Name: "molr", Permissions: "ask", Mode: "auto"}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.Command = args[0]
		args = args[1:]
		if o.Command != "update" && o.Command != "uninstall" && o.Command != "status" {
			return o, fmt.Errorf("unknown command %q; omit a subcommand to install the current package", o.Command)
		}
	}
	f := flag.NewFlagSet("molr-setup", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	f.BoolVar(&o.Interactive, "interactive", false, "require a terminal")
	f.BoolVar(&o.NonInteractive, "non-interactive", false, "never prompt or open a browser")
	f.BoolVar(&o.DryRun, "dry-run", false, "show the plan without installation changes")
	f.BoolVar(&o.Yes, "yes", false, "confirm the plan and requested system preparation; permissions have their own policy")
	f.StringVar(&o.Name, "name", "molr", "instance service name")
	f.StringVar(&o.User, "user", "", "system service account; requires root")
	f.StringVar(&o.ProgramRoot, "program-root", "", "program base directory; instance gets its own child")
	f.StringVar(&o.Config, "config", "", "initial JSON configuration; existing configuration is preserved")
	f.StringVar(&o.Record, "record", "", "explicit installation.json to manage")
	f.StringVar(&o.Permissions, "directory-permissions", "ask", "ask, fix, or ignore writable directories")
	f.StringVar(&o.Source, "source", "", "release source base URL for future updates")
	f.StringVar(&o.Channel, "channel", "", "stable, preview, or nightly update subscription")
	f.StringVar(&o.Version, "version", "", "update to an explicit version (update command only)")
	f.IntVar(&o.Port, "port", 0, "initial port; default first free port from 8787")
	f.StringVar(&o.PublicHost, "public-host", "", "initial private IPv4 address")
	f.StringVar(&o.InitOwner, "init-owner", "", "initialize owner for a custom configuration; password from stdin")
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
	if o.Interactive && o.NonInteractive {
		return o, fmt.Errorf("choose either --interactive or --non-interactive")
	}
	if o.Interactive {
		o.Mode = "interactive"
	}
	if o.NonInteractive {
		o.Mode = "non-interactive"
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
	ui, err := newUI(o.Mode, o.DryRun, out, diagnostics)
	if err != nil {
		return err
	}
	defer ui.close()
	ui.Yes = o.Yes
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	e := &Engine{Ctx: ctx, Options: o, UI: ui, Input: input, Executable: executable, Root: filepath.Dir(filepath.Dir(executable)), Build: buildinfo.Current(), Command: command}
	if err = e.execute(); err != nil {
		fmt.Fprintf(diagnostics, "\nStopped during: %s\nState: %s\n", ui.Stage, ui.State)
	}
	return err
}
func (e *Engine) packageAt(root string) (*Package, error) {
	for _, name := range []string{"release.json", "LICENSE", "THIRD_PARTY_NOTICES.md", "bin/molr", "bin/molr-setup"} {
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
	for _, name := range []string{"molr", "molr-setup"} {
		if err = staticELF(filepath.Join(root, "bin", name), b.Arch); err != nil {
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
	installedPath := filepath.Join(e.Root, "installation.json")
	if _, err := os.Lstat(installedPath); err == nil {
		r, err := readRecord(installedPath, true)
		if err != nil {
			return err
		}
		if e.Options.Record != "" && e.Options.Record != installedPath {
			return fmt.Errorf("installed tool manages its own instance; use that instance's tool")
		}
		if e.Options.NameSet && e.Options.Name != r.Name {
			return fmt.Errorf("--name does not match installed instance")
		}
		if e.Options.Command == "uninstall" {
			return e.uninstall(r)
		}
		if e.Options.Command == "update" {
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
	switch e.Options.Command {
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
	e.UI.print("Instance: %s\nVersion: %s\nState: %s\nProgram: %s\nUpdates: %s", r.Name, r.Build.Version, r.State, r.Paths.Program, r.UpdateSource())
	if e.Options.Command == "status" || e.Options.DryRun || !e.UI.interactive() {
		return nil
	}
	answer, err := e.UI.answer("[u] Check/update, [r] repair, [x] uninstall, [q] exit: ")
	if err != nil {
		return err
	}
	switch answer {
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
func layout(name, home string, system bool, programRoot string) (installrecord.Paths, string, error) {
	instance, err := installrecord.Instance(name)
	if err != nil {
		return installrecord.Paths{}, "", err
	}
	envPath := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}
	var p installrecord.Paths
	if system {
		if programRoot == "" {
			programRoot = "/opt/molr"
		}
		p = installrecord.Paths{Program: filepath.Join(programRoot, instance), Config: filepath.Join("/etc/molr", instance), Data: filepath.Join("/var/lib/molr", instance), Cache: filepath.Join("/var/cache/molr", instance), Unit: filepath.Join("/etc/systemd/system", name+".service")}
	} else {
		if programRoot == "" {
			programRoot = filepath.Join(home, ".local/lib/molr")
		}
		c := envPath("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		d := envPath("XDG_DATA_HOME", filepath.Join(home, ".local/share"))
		k := envPath("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
		p = installrecord.Paths{Program: filepath.Join(programRoot, instance), Config: filepath.Join(c, "molr", instance), Data: filepath.Join(d, "molr", instance), Cache: filepath.Join(k, "molr", instance), Unit: filepath.Join(c, "systemd/user", name+".service")}
	}
	for _, v := range []string{programRoot, p.Program, p.Config, p.Data, p.Cache, p.Unit} {
		if err = checkPath(v); err != nil {
			return p, "", err
		}
	}
	return p, programRoot, nil
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
	uid := os.Geteuid()
	var r installrecord.Record
	if previous != nil {
		r = *previous
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
		r = installrecord.Record{Schema: 1, State: "installing", Name: o.Name, User: a.Name, UID: a.UID, GID: a.GID, OwnerUID: uid, System: system, Paths: p, ProgramRoot: base, DataDir: p.Data, ManagedData: true, Updates: installrecord.Updates{Source: "https://molr.ai/releases", Channel: channelFor(pkg.Build.Version)}}
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
	sandbox, err := object(cfg, "sandbox")
	if err != nil {
		return nil, err
	}
	if _, ok := sandbox["host_tools"]; !ok {
		sandbox["host_tools"] = sandbox["rootfs"] == nil || sandbox["rootfs"] == ""
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
	}
	if o.Config != "" && previous == nil && !o.NoStart && o.InitOwner == "" {
		return nil, fmt.Errorf("custom configuration needs --init-owner NAME (password on stdin), or --no-start")
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
		if n == 0 && pkg.Build.Commit == p.Previous.Build.Commit && !e.Options.DryRun {
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
	return e.apply(p)
}
