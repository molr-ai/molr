package sandbox

import (
	"fmt"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Keep policy generation platform-independent so malformed scopes are tested on Linux too.
func seatbeltPath(p string) (string, error) {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || !utf8.ValidString(p) || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "", core.Invalid("path", "clean absolute sandbox path required")
	}
	return strconv.Quote(p), nil
}
func validateSeatbelt(c platform.Command) error {
	if c.Script == "" || len(c.Script) > 65536 || strings.ContainsRune(c.Script, 0) || strings.ContainsRune(c.Directory, 0) || c.Timeout < 0 || (!c.Managed && (c.Timeout == 0 || c.Timeout > time.Hour)) || c.MaxOutputBytes < 1 || c.MaxOutputBytes > 128<<10 {
		return core.Invalid("sandbox", "invalid native execution plan")
	}
	if c.Executable != "" || len(c.Args) > 0 || len(c.Environment) > 0 || len(c.ReadRoots) > 0 || len(c.WriteRoots) > 0 || len(c.AllowedOrigins) > 0 || len(c.GPUDeviceIDs) > 0 || len(c.NetworkTargets) > 0 || c.ComputeQuota || c.WorkspaceConstraints != nil || !reflect.DeepEqual(c.Limits, platform.Limits{}) {
		return core.Invalid("sandbox", "unsupported native execution option")
	}
	if c.BrowserAssets || c.BrowserProxy != nil {
		if !c.BrowserAssets || c.BrowserProxy == nil || !c.Managed || c.FullAccess || c.TTY || c.Script != "exec /molr/helper sandbox-helper browser" || c.Directory != "/work" || !slices.Equal(c.ForwardPorts, []int{browser.CommandPort, browser.StreamPort}) {
			return core.Invalid("browser", "trusted browser launch required")
		}
		proxy := *c.BrowserProxy
		if err := proxy.Validate(); err != nil {
			return core.Invalid("proxy", err.Error())
		}
		assets := map[string]bool{}
		for _, m := range c.Mounts {
			if m.Destination == "/resources/browser-agent" || m.Destination == "/resources/browser-chromium" {
				if !m.ReadOnly || assets[m.Destination] {
					return core.Invalid("browser", "read-only browser assets required")
				}
				assets[m.Destination] = true
			}
		}
		if len(assets) != 2 {
			return core.Invalid("browser", "browser assets required")
		}
	}
	for _, n := range c.NetworkAuthorizations {
		if n.Constraints != nil || n.GroupConstraints != nil {
			return core.Invalid("network", "native network quotas are unsupported")
		}
	}
	if g := c.GroupComputeConstraints; g != nil {
		if g.CPUPercent != 0 || g.MemoryBytes != 0 || g.Processes != 0 || g.TempBytes != 0 || len(g.GPUDevices()) > 0 {
			return core.Invalid("compute", "native group compute supports duration only")
		}
		if err := (&core.ResourceConstraints{Compute: g}).Validate(); err != nil {
			return err
		}
	}
	if err := platform.ValidateForwardPorts(c.ForwardPorts); err != nil {
		return err
	}
	if len(c.ForwardPorts) > 0 && !c.NetworkAccess && !c.BrowserAssets {
		return core.Invalid("forward_ports", "network permission required")
	}
	if !c.BrowserAssets {
		for _, port := range c.ForwardPorts {
			if port >= browserControlFirst && port < browserControlLast {
				return core.Invalid("forward_ports", "browser control ports are reserved")
			}
		}
	}
	if len(c.Mounts) > 32 {
		return core.Invalid("files", "too many file scopes")
	}
	for _, m := range c.Mounts {
		if _, err := seatbeltPath(m.Source); err != nil {
			return err
		}
	}
	return nil
}

const seatbeltBase = `(version 1)
(deny default)
(deny file-write* (regex #"^/private/tmp/molr-ipc-[^/]+(/|$)"))
(allow process-exec)
(allow process-fork)
(allow signal (target same-sandbox))
(deny process-info*)
(allow process-info* (target same-sandbox))
(allow sysctl-read (sysctl-name-prefix "hw.") (sysctl-name-prefix "machdep.cpu.") (sysctl-name "kern.osrelease") (sysctl-name "kern.osversion") (sysctl-name "kern.ostype") (sysctl-name "kern.hostname") (sysctl-name "kern.maxfilesperproc") (sysctl-name "kern.argmax") (sysctl-name "kern.boottime"))
(allow file-read-metadata)
(allow file-read-data (literal "/"))
(allow file-read* (subpath "/System/Library") (subpath "/usr/lib") (subpath "/usr/share") (subpath "/bin") (subpath "/usr/bin") (subpath "/sbin") (subpath "/usr/sbin") (literal "/private/var/db/xcode_select_link") (subpath "/private/etc/ssl"))
(allow file-read* file-write* (literal "/dev/null") (literal "/dev/zero") (literal "/dev/tty"))
(allow file-read* (literal "/dev/random") (literal "/dev/urandom"))
`
const seatbeltNetwork = `(allow network-outbound (remote tcp) (remote udp))
(allow network-bind network-inbound (local tcp) (local udp))
(allow network-outbound (literal "/private/var/run/mDNSResponder"))
(allow system-socket (require-all (socket-domain AF_SYSTEM) (socket-protocol 2)))
(allow mach-lookup (global-name "com.apple.bsd.dirhelper") (global-name "com.apple.system.opendirectoryd.libinfo") (global-name "com.apple.system.opendirectoryd.membership") (global-name "com.apple.SecurityServer") (global-name "com.apple.networkd") (global-name "com.apple.ocspd") (global-name "com.apple.trustd") (global-name "com.apple.trustd.agent") (global-name "com.apple.mDNSResponder") (global-name "com.apple.mDNSResponderHelper") (global-name "com.apple.SystemConfiguration.DNSConfiguration") (global-name "com.apple.SystemConfiguration.configd"))
(allow file-read* (literal "/private/etc/hosts") (literal "/private/etc/resolv.conf") (subpath "/private/etc/ssl") (subpath "/private/var/db/mds"))
`

func seatbeltProfile(read, write []string, tty string, network bool) (string, error) {
	return seatbeltProfilePorts(read, write, tty, network, nil)
}

func seatbeltProfilePorts(read, write []string, tty string, network bool, ports []int) (string, error) {
	var b strings.Builder
	b.WriteString(seatbeltBase)
	for _, set := range []struct {
		paths      []string
		operations string
	}{{read, "file-read*"}, {write, "file-read* file-write*"}} {
		for _, p := range set.paths {
			q, err := seatbeltPath(p)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "(allow %s (subpath %s))\n", set.operations, q)
		}
	}
	if tty != "" {
		q, err := seatbeltPath(tty)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "(allow file-read* file-write* file-ioctl (literal %s))\n", q)
	}
	if network {
		b.WriteString(seatbeltNetwork)
	}
	// These host ports carry privileged browser control. Deny every destination
	// address (including 0.0.0.0 and IPv6 aliases), and prevent other tasks binding
	// them. Only a browser's own pair is reachable within its Seatbelt boundary.
	for port := browserControlFirst; port < browserControlLast; port++ {
		if slices.Contains(ports, port) {
			fmt.Fprintf(&b, "(allow network-bind network-inbound (local tcp \"localhost:%d\"))\n(allow network-outbound (remote tcp \"localhost:%d\"))\n", port, port)
		} else {
			fmt.Fprintf(&b, "(deny network-outbound (remote tcp \"*:%d\"))\n(deny network-bind network-inbound (local tcp \"*:%d\"))\n", port, port)
		}
	}
	return b.String(), nil
}

// A bounded shared range also isolates browsers belonging to other Runtime instances.
const browserControlFirst = 39200
const browserControlLast = 39264
