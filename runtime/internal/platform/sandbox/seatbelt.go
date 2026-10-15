package sandbox

import (
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"path/filepath"
	"reflect"
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
	if c.BrowserAssets || c.BrowserProxy != nil || c.Executable != "" || len(c.Args) > 0 || len(c.Environment) > 0 || len(c.ReadRoots) > 0 || len(c.WriteRoots) > 0 || len(c.AllowedOrigins) > 0 || len(c.GPUDeviceIDs) > 0 || len(c.NetworkTargets) > 0 || c.ComputeQuota || c.WorkspaceConstraints != nil || !reflect.DeepEqual(c.Limits, platform.Limits{}) {
		return core.Invalid("sandbox", "unsupported native execution option")
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
	if len(c.ForwardPorts) > 0 && !c.NetworkAccess {
		return core.Invalid("forward_ports", "network permission required")
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
	return b.String(), nil
}
