package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"molr.ai/runtime/internal/installrecord"
)

type Account struct {
	Name, Home string
	UID, GID   int
}

func lookupAccount(name string) (*Account, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.Atoi(u.Gid)
	return &Account{u.Username, u.HomeDir, uid, gid}, err
}
func currentAccount() (*Account, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	return lookupAccount(u.Username)
}
func command(ctx context.Context, args []string, input io.Reader, env []string, out, diagnostics io.Writer) ([]byte, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = input
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	if out == nil {
		cmd.Stdout = &stdout
	} else {
		cmd.Stdout = out
	}
	if diagnostics == nil {
		cmd.Stderr = &stderr
	} else {
		cmd.Stderr = diagnostics
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return stdout.Bytes(), ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 4096 {
			detail = detail[:4096]
		}
		return stdout.Bytes(), fmt.Errorf("%s failed: %w %s", filepath.Base(args[0]), err, detail)
	}
	return stdout.Bytes(), nil
}
func (e *Engine) run(args []string, capture bool) ([]byte, error) {
	if capture {
		return e.Command(e.Ctx, args, nil, nil, nil, nil)
	}
	return e.Command(e.Ctx, args, nil, nil, e.UI.Out, e.UI.Err)
}
func control(r *installrecord.Record) []string {
	if r.System {
		return []string{"systemctl"}
	}
	return []string{"systemctl", "--user"}
}
func (e *Engine) service(r *installrecord.Record, args ...string) error {
	_, err := e.run(append(control(r), args...), false)
	return err
}
func asUser(r *installrecord.Record, args ...string) []string {
	if r.System {
		return append([]string{"runuser", "-u", r.User, "--"}, args...)
	}
	return args
}
func hostLabel() string {
	data, _ := os.ReadFile("/etc/os-release")
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(v, `"`) {
			if s, err := strconv.Unquote(v); err == nil {
				v = s
			}
		}
		values[k] = v
	}
	return strings.TrimSpace(values["ID"] + " " + values["VERSION_ID"])
}
func staticBusybox() string {
	for _, p := range []string{"/usr/bin/busybox", "/bin/busybox"} {
		real, err := filepath.EvalSymlinks(p)
		if err == nil && staticELF(real, "") == nil {
			return real
		}
	}
	return ""
}
func missingDependencies() []string {
	var missing []string
	for _, name := range []string{"bwrap", "nsenter", "setpriv"} {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if staticBusybox() == "" {
		missing = append(missing, "static BusyBox")
	}
	if _, err := os.Stat("/etc/ssl/certs/ca-certificates.crt"); err != nil {
		missing = append(missing, "CA certificates")
	}
	return missing
}
func (e *Engine) privileged() ([]string, error) {
	if os.Geteuid() == 0 {
		return nil, nil
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return nil, fmt.Errorf("administrator privileges required; prepare dependencies as root and retry with --skip-dependencies")
	}
	if e.UI.interactive() {
		return []string{"sudo"}, nil
	}
	return []string{"sudo", "-n"}, nil
}
func (e *Engine) apt(packages ...string) error {
	if _, err := exec.LookPath("apt-get"); err != nil {
		return fmt.Errorf("automatic preparation for this package manager is not implemented; prepare the listed capabilities manually")
	}
	prefix, err := e.privileged()
	if err != nil {
		return err
	}
	prefix = append(prefix, "env", "DEBIAN_FRONTEND=noninteractive", "apt-get")
	if _, err = e.run(append(append([]string{}, prefix...), "update"), false); err != nil {
		return err
	}
	_, err = e.run(append(append(append([]string{}, prefix...), "install", "-y", "-o", "Dpkg::Options::=--force-confold"), packages...), false)
	return err
}
func (e *Engine) prepareDependencies(sandbox bool) error {
	if !sandbox {
		return nil
	}
	missing := missingDependencies()
	if len(missing) == 0 {
		return nil
	}
	e.UI.print("Missing dependencies: %s", strings.Join(missing, ", "))
	if e.Options.SkipDependencies {
		return nil
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		e.UI.print("Prepare these capabilities with your system package manager.")
		return nil
	}
	if !e.Options.InstallDeps && !e.UI.interactive() {
		return fmt.Errorf("dependency preparation requires --install-deps, or prepare dependencies manually")
	}
	ok, err := e.UI.confirm("Install system dependencies with APT?")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("dependency preparation declined")
	}
	if err = e.apt("bubblewrap", "busybox-static", "ca-certificates", "util-linux"); err != nil {
		return err
	}
	if missing = missingDependencies(); len(missing) > 0 {
		return fmt.Errorf("dependencies still missing after preparation: %s", strings.Join(missing, ", "))
	}
	return nil
}
func (e *Engine) probeBwrap(r *installrecord.Record) error {
	ctx, cancel := context.WithTimeout(e.Ctx, 15*time.Second)
	defer cancel()
	args := []string{"/usr/bin/bwrap", "--unshare-all", "--unshare-user", "--cap-drop", "ALL", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--", "/usr/bin/true"}
	// Match Runtime's preference before proposing any policy installation. A
	// missing aa-exec or unloaded chrome profile simply fails this trusted probe.
	chrome := append([]string{"setpriv", "--no-new-privs", "/usr/bin/aa-exec", "-p", "chrome", "--"}, args...)
	if _, err := e.Command(ctx, asUser(r, chrome...), nil, nil, nil, nil); err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := e.Command(ctx, asUser(r, append([]string{"setpriv", "--no-new-privs"}, args...)...), nil, nil, nil, nil)
	return err
}
func (e *Engine) preparePolicy(r *installrecord.Record) error {
	if e.Options.SkipDependencies {
		return nil
	}
	flag, err := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns")
	if err != nil || strings.TrimSpace(string(flag)) != "1" {
		return nil
	}
	if err = e.probeBwrap(r); err == nil {
		return nil
	}
	e.UI.print("Bubblewrap preflight: %v", err)
	if !e.Options.InstallDeps && !e.UI.interactive() {
		return fmt.Errorf("prepare the Bubblewrap AppArmor policy or use --install-deps")
	}
	const profile = "/etc/apparmor.d/bwrap-userns-restrict"
	const extra = "/usr/share/apparmor/extra-profiles/bwrap-userns-restrict"
	entries, _ := os.ReadDir("/etc/apparmor.d")
	for _, entry := range entries {
		p := filepath.Join("/etc/apparmor.d", entry.Name())
		if p == profile || entry.IsDir() {
			continue
		}
		data, _ := os.ReadFile(p)
		if bytes.Contains(data, []byte("bwrap")) {
			return fmt.Errorf("existing Bubblewrap policy requires administrator review: %s", p)
		}
	}
	if err = checkPath(profile); err != nil {
		return err
	}
	ok, err := e.UI.confirm("Prepare and load the distribution Bubblewrap AppArmor policy (preserve existing policies)?")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("AppArmor preparation declined")
	}
	_, a := os.Stat(profile)
	_, b := os.Stat(extra)
	if a != nil && b != nil {
		if err = e.apt("apparmor", "apparmor-profiles"); err != nil {
			return err
		}
	}
	if _, err = exec.LookPath("apparmor_parser"); err != nil {
		if err = e.apt("apparmor"); err != nil {
			return err
		}
	}
	prefix, err := e.privileged()
	if err != nil {
		return err
	}
	if _, err = os.Stat(profile); os.IsNotExist(err) {
		if _, err = e.run(append(append([]string{}, prefix...), "install", "-m", "0644", extra, profile), false); err != nil {
			return err
		}
	}
	if _, err = e.run(append(prefix, "apparmor_parser", "-r", profile), false); err != nil {
		return err
	}
	return e.probeBwrap(r)
}
func (e *Engine) checkServiceHost(r *installrecord.Record, sandbox bool) (bool, error) {
	for _, name := range append([]string{"systemctl"}, map[bool][]string{true: {"useradd", "runuser"}}[r.System]...) {
		if _, err := exec.LookPath(name); err != nil {
			return false, fmt.Errorf("missing system tool: %s", name)
		}
	}
	data, err := e.run([]string{"systemctl", "--version"}, true)
	if err != nil {
		return false, err
	}
	var version int
	fmt.Sscanf(string(data), "systemd %d", &version)
	delegate := version >= 254
	if sandbox {
		if !delegate {
			return false, fmt.Errorf("the current sandbox service requires systemd 254+ delegation; found %d", version)
		}
		if _, err = os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
			return false, fmt.Errorf("cgroup v2 is required for sandbox resources")
		}
		if missing := missingDependencies(); len(missing) > 0 {
			return false, fmt.Errorf("sandbox dependencies unavailable: %s", strings.Join(missing, ", "))
		}
	}
	if _, err = e.run(append(control(r), "show-environment"), true); err != nil {
		return false, fmt.Errorf("service manager unavailable; for user installation log in via a normal SSH/desktop session: %w", err)
	}
	return delegate, nil
}
func unitQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
}
func renderUnit(r *installrecord.Record, delegate bool) string {
	identity, target := "", "default.target"
	if r.System {
		identity = fmt.Sprintf("User=%s\nGroup=%d\n", r.User, r.GID)
		target = "multi-user.target"
	}
	delegation := ""
	if delegate {
		delegation = "Delegate=yes\nDelegateSubgroup=supervisor\n"
	}
	return fmt.Sprintf("[Unit]\nDescription=Molr Runtime\n\n[Service]\nType=exec\n%s%sKillMode=control-group\nUMask=0077\nNoNewPrivileges=yes\nRestart=on-failure\nTimeoutStopSec=30\nEnvironment=%s\nEnvironment=%s\nExecStart=%s serve -config %s\n\n[Install]\nWantedBy=%s\n", identity, delegation, unitQuote("TMPDIR="+filepath.Join(r.Paths.Cache, "tmp")), unitQuote("XDG_CACHE_HOME="+r.Paths.Cache), unitQuote(filepath.Join(r.Paths.Program, "bin/molr")), unitQuote(filepath.Join(r.Paths.Config, "server.json")), target)
}
func defaultHTTP(port int, host string) (map[string]any, error) {
	addresses := []string{"127.0.0.1"}
	preferredIndex, metric := 0, int(^uint(0)>>1)
	routes, _ := netlink.RouteList(nil, netlink.FAMILY_V4)
	for _, route := range routes {
		if route.Dst == nil && route.LinkIndex > 0 && route.Priority < metric {
			preferredIndex, metric = route.LinkIndex, route.Priority
		}
	}
	preferredAddress := ""
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 {
				continue
			}
			items, _ := iface.Addrs()
			for _, item := range items {
				ip, _, err := net.ParseCIDR(item.String())
				if err == nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
					addresses = append(addresses, ip.String())
					if iface.Index == preferredIndex && preferredAddress == "" {
						preferredAddress = ip.String()
					}
				}
			}
		}
	}
	sort.Strings(addresses[1:])
	addresses = unique(addresses)
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() == nil || (!ip.IsPrivate() && !ip.IsLoopback()) || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			return nil, fmt.Errorf("--public-host requires a private or loopback IPv4 address")
		}
		addresses = unique(append(addresses, host))
	}
	if port == 0 {
		for p := 8787; p < 8888; p++ {
			listener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", p))
			if err == nil {
				listener.Close()
				port = p
				break
			}
		}
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("no valid port; use --port 1..65535")
	}
	if host == "" {
		host = addresses[0]
		if preferredAddress != "" {
			host = preferredAddress
		} else if len(addresses) > 1 {
			host = addresses[1]
		}
	}
	origins := []string{}
	for _, a := range addresses {
		origins = append(origins, fmt.Sprintf("http://%s:%d", a, port))
	}
	return map[string]any{"listen": fmt.Sprintf("0.0.0.0:%d", port), "public_url": fmt.Sprintf("http://%s:%d", host, port), "tls_mode": "lan", "allowed_origins": origins}, nil
}
func unique(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}
func jsonBytes(v any) []byte { data, _ := json.MarshalIndent(v, "", "  "); return append(data, '\n') }

// Only an explicit CLI --install-deps reaches package/policy preparation.
// Runtime maintenance never supplies that flag and never invokes sudo.
func (e *Engine) prepareBrowserDependencies(r *installrecord.Record) error {
	if !e.Options.InstallDeps {
		return nil
	}
	// Prefer the distribution's t64 package names where available.
	names := []string{"libnss3", "libnspr4", "libatk1.0-0", "libatk-bridge2.0-0", "libcups2", "libdrm2", "libdbus-1-3", "libxcb1", "libxkbcommon0", "libx11-6", "libxcomposite1", "libxdamage1", "libxext6", "libxfixes3", "libxrandr2", "libgbm1", "libasound2", "libpango-1.0-0", "libcairo2", "fonts-liberation", "ca-certificates"}
	for i, name := range names {
		switch name {
		case "libatk1.0-0", "libatk-bridge2.0-0", "libcups2", "libasound2":
			if b, err := e.run([]string{"apt-cache", "show", name + "t64"}, true); err == nil && bytes.Contains(b, []byte("Package:")) {
				names[i] = name + "t64"
			}
		}
	}
	if err := e.apt(names...); err != nil {
		return err
	}
	if err := e.prepareDependencies(true); err != nil {
		return err
	}
	return e.preparePolicy(r)
}
