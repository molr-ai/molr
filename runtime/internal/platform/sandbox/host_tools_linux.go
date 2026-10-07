//go:build linux && (amd64 || arm64)

package sandbox

import (
	"os"
	"path/filepath"
)

// The entire Runtime already runs as the selected deployment account. Sandboxed
// children inherit that host identity; there is no account switching here.
func checkHostToolsIdentity() error {
	if os.Getuid() == 0 || os.Getuid() != os.Geteuid() || os.Getgid() != os.Getegid() {
		return unavailable("sandbox_host_tools_identity_required")
	}
	return nil
}

// No command probing or host program execution is performed here. Files remain
// subject to the selected account's DAC/ACL permissions at each actual access.
func appendHostToolsMounts(args *[]string, add func(*os.File) string, protected ...string) error {
	for _, destination := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/alternatives", "/etc/fonts", "/etc/ImageMagick-6", "/etc/ImageMagick-7", "/etc/ssl/openssl.cnf", "/etc/ld.so.cache"} {
		source, err := filepath.EvalSymlinks(destination)
		if os.IsNotExist(err) && destination != "/usr" && destination != "/bin" && destination != "/lib" {
			continue
		}
		if err != nil {
			return unavailable("sandbox_host_tools_unavailable")
		}
		for _, private := range protected {
			if private != "" && (beneath(private, source) || beneath(source, private)) {
				return unavailable("sandbox_host_tools_private_overlap")
			}
		}
		info, err := os.Stat(source)
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			return unavailable("sandbox_host_tools_unavailable")
		}
		if info.IsDir() {
			if err := rejectSubmounts(source); err != nil {
				return err
			}
		}
		f, err := pin(source, info.IsDir())
		if err != nil {
			return unavailable("sandbox_host_tools_unavailable")
		}
		*args = append(*args, "--ro-bind-fd", add(f), destination)
	}
	return nil
}
