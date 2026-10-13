package browser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// ProxySummary is safe for task records and model-visible status. It contains no credentials.
type ProxySummary struct {
	Mode           string
	Server         string
	Bypass         []string
	Authentication bool
	Version        uint64
}

// Proxy is supplied only by the trusted browser planner; credentials never marshal.
type Proxy struct {
	ProxySummary
	Username string `json:"-"`
	Password string `json:"-"`
}

func (p *Proxy) Validate() error {
	if p.Mode != "direct" && p.Mode != "proxy" {
		return fmt.Errorf("select direct or proxy")
	}
	if len(p.Server) > 2048 || len(p.Bypass) > 64 || len(p.Username) > 256 || len(p.Password) > 1024 {
		return fmt.Errorf("proxy settings too long")
	}
	for _, v := range []string{p.Server, p.Username, p.Password} {
		if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return fmt.Errorf("control characters are not allowed")
		}
	}
	if p.Mode == "direct" {
		if p.Server != "" || len(p.Bypass) > 0 || p.Username != "" || p.Password != "" {
			return fmt.Errorf("direct mode cannot contain proxy settings")
		}
		p.Authentication = false
		return nil
	}
	u, err := url.Parse(p.Server)
	if err != nil || (u.Scheme != "http" && u.Scheme != "socks5") || u.User != nil || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
		return fmt.Errorf("use an http:// or socks5:// host:port without credentials or a path")
	}
	if strings.ContainsAny(u.Hostname(), " /\\?#@") {
		return fmt.Errorf("invalid proxy host")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("proxy port must be between 1 and 65535")
	}
	p.Server = u.Scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), strconv.Itoa(port))
	if (p.Username == "") != (p.Password == "") {
		return fmt.Errorf("both username and password are required")
	}
	if u.Scheme == "socks5" && (p.Username != "" || p.Password != "") {
		return fmt.Errorf("SOCKS5 authentication is not supported")
	}
	p.Authentication = p.Username != ""
	for i, entry := range p.Bypass {
		entry = strings.TrimSpace(entry)
		// Keep the documented bypass subset small: hosts, wildcard domains, IPs and CIDRs.
		if entry == "" || len(entry) > 256 || strings.ContainsAny(entry, ";,<>@\\?#") || strings.IndexFunc(entry, unicode.IsSpace) >= 0 || strings.IndexFunc(entry, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid proxy bypass entry")
		}
		if strings.Contains(entry, "://") {
			return fmt.Errorf("bypass entries must be hosts or IP ranges")
		}
		p.Bypass[i] = entry
	}
	return nil
}

// LaunchConfig is mounted from a private memory FD. It is never an operation payload.
func (p Proxy) LaunchConfig() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	server := p.Server
	if p.Authentication {
		u, _ := url.Parse(server)
		u.User = url.UserPassword(p.Username, p.Password)
		server = u.String()
	}
	return json.Marshal(struct {
		Proxy  string `json:"proxy"`
		Bypass string `json:"proxyBypass"`
	}{server, strings.Join(p.Bypass, ",")})
}

func ProxyErrorReason(message string) string {
	for _, pair := range [][2]string{
		{"ERR_PROXY_AUTH", "proxy_authentication_failed"}, {"ERR_INVALID_AUTH_CREDENTIALS", "proxy_authentication_failed"},
		{"ERR_PROXY_CONNECTION_FAILED", "proxy_connection_failed"}, {"ERR_TUNNEL_CONNECTION_FAILED", "proxy_tunnel_failed"},
		{"ERR_SOCKS_CONNECTION_FAILED", "proxy_connection_failed"}, {"ERR_CERT_", "network_certificate_failed"}, {"ERR_SSL_PROTOCOL_ERROR", "network_tls_failed"}, {"ERR_NAME_NOT_RESOLVED", "network_dns_failed"},
		{"ERR_CONNECTION_REFUSED", "network_connection_refused"}, {"ERR_CONNECTION_TIMED_OUT", "network_timeout"},
		{"ERR_TIMED_OUT", "network_timeout"}, {"ERR_INTERNET_DISCONNECTED", "network_offline"},
	} {
		if strings.Contains(message, pair[0]) {
			return pair[1]
		}
	}
	return "network_failure_unknown"
}

// RedactProxyError also covers escaped errors emitted before CLI JSON decoding.
func RedactProxyError(message, server string) string {
	u, err := url.Parse(server)
	if err != nil || u.User == nil {
		return message
	}
	username := u.User.Username()
	password, _ := u.User.Password()
	secrets := []string{server, u.User.String(), base64.StdEncoding.EncodeToString([]byte(username + ":" + password))}
	for _, secret := range []string{password, username} {
		encoded, _ := json.Marshal(secret)
		secrets = append(secrets, url.QueryEscape(secret), url.PathEscape(secret), string(encoded[1:len(encoded)-1]), secret)
	}
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}
