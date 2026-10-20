package sandbox

import (
	"fmt"
	"strings"
)

func seatbeltBrowserProfile(read, write []string, temp string, network bool, ports []int) (string, error) {
	profile, err := seatbeltProfilePorts(read, write, "", network, ports)
	if err != nil {
		return "", err
	}
	q, err := seatbeltPath(temp)
	if err != nil {
		return "", err
	}
	return profile + fmt.Sprintf("(allow network-bind network-inbound network-outbound (subpath %s))\n", q) + strings.TrimSpace(seatbeltBrowserServices) + "\n", nil
}

// Chrome runs with --no-sandbox inside Molr's mandatory Seatbelt. macOS cannot
// initialize Chromium's nested Seatbelt within an effective outer sandbox.
// No user settings, entitlements, SIP changes or TCC permission prompts needed.
const seatbeltBrowserServices = `
(allow file-read* (literal "/Library/Apple/usr/libexec/oah/libRosettaRuntime"))
(allow mach-lookup (global-name "com.apple.system.notification_center") (global-name "com.apple.logd") (global-name "com.apple.coreservices.launchservicesd") (global-name "com.apple.distributed_notifications@Uv3") (global-name "com.apple.tccd.system") (global-name "com.apple.windowserver.active"))
(allow ipc-posix-shm-read-data (ipc-posix-name "apple.shm.notification_center"))
(allow sysctl-read (sysctl-name "kern.secure_kernel") (sysctl-name "kern.willshutdown") (sysctl-name "kern.osproductversion") (sysctl-name "kern.osvariant_status") (sysctl-name "security.mac.lockdown_mode_state") (sysctl-name "kern.bootargs"))
(allow file-read* (literal "/dev/autofs_nowait") (subpath "/private/var/db/timezone"))
(allow mach-lookup (global-name "com.apple.CARenderServer") (global-name "com.apple.CoreServices.coreservicesd") (global-name "com.apple.DiskArbitration.diskarbitrationd"))
(allow system-audit)
(allow sysctl-read (sysctl-name "machdep.ptrauth_enabled") (sysctl-name "kern.version"))
(allow sysctl-read (sysctl-name "kern.hv_vmm_present"))
(allow mach-register mach-lookup
 (global-name-regex #"^com\.google\.Chrome\.MachPortRendezvousServer\.[0-9]+$")
 (global-name-regex #"^com\.google\.chrome\.for\.testing\.MachPortRendezvousServer\.[0-9]+$")
 (global-name-regex #"^org\.chromium\.crashpad\.child_port_handshake\..*$"))
(allow iokit-open (iokit-user-client-class "RootDomainUserClient"))
`
