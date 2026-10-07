package config

import (
	"molr.ai/runtime/internal/core"
	"net"
	"net/url"
	"strings"
)

// PortForward controls Runtime listeners for both local and remote shell tasks.
// A listener is created only when a task explicitly requests a port.
type PortForward struct {
	Enabled    bool   `json:"enabled"`
	ListenHost string `json:"listen_host"`
	PublicHost string `json:"public_host"`
}

func (f PortForward) validate() error {
	if !f.Enabled {
		return nil
	}
	if net.ParseIP(f.ListenHost) == nil {
		return core.Invalid("port_forward.listen_host", "IP address without port required")
	}
	if f.PublicHost != "" {
		u, err := url.Parse("http://" + net.JoinHostPort(f.PublicHost, "80"))
		if err != nil || u.Hostname() != f.PublicHost || strings.ContainsAny(f.PublicHost, "/\\@?#% \t\r\n") || strings.Contains(f.PublicHost, ":") && net.ParseIP(f.PublicHost) == nil {
			return core.Invalid("port_forward.public_host", "hostname or IP without scheme or port required")
		}
		if ip := net.ParseIP(f.PublicHost); ip != nil && (ip.IsUnspecified() || ip.IsMulticast()) {
			return core.Invalid("port_forward.public_host", "reachable address required")
		}
	}
	return nil
}
