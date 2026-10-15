//go:build linux || darwin

package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"molr.ai/runtime/internal/installrecord"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
func asUser(r *installrecord.Record, args ...string) []string {
	if r.System {
		return append([]string{"runuser", "-u", r.User, "--"}, args...)
	}
	return args
}
func defaultHTTP(port int, host string) (map[string]any, error) {
	addresses := []string{"127.0.0.1"}
	preferredIndex := preferredInterface()
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
