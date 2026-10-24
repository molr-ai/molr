// Package config defines validated fixed-server settings without secret material.
package config

import (
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/logging"
)

type TLSMode string

const (
	DirectTLS    TLSMode = "direct"
	TrustedProxy TLSMode = "proxy"
	// LocalHTTP is explicit development-only plaintext on a numeric loopback listener.
	LocalHTTP TLSMode = "local"
	LANHTTP   TLSMode = "lan"
)

// Duration serializes configuration durations as strings such as "30s".
type Duration time.Duration

func (d Duration) Value() time.Duration         { return time.Duration(d) }
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }
func (d *Duration) UnmarshalJSON(b []byte) error {
	var value string
	if err := json.Unmarshal(b, &value); err != nil {
		return core.Invalid("duration", "duration must be a string")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return core.Invalid("duration", "invalid duration")
	}
	*d = Duration(parsed)
	return nil
}

type HTTP struct {
	AutoRenewCertificate bool     `json:"auto_renew_certificate"`
	Listen               string   `json:"listen"`
	PublicURL            string   `json:"public_url"`
	TLSMode              TLSMode  `json:"tls_mode"`
	CertFile             string   `json:"cert_file"`
	KeyFile              string   `json:"key_file"`
	TrustedProxies       []string `json:"trusted_proxies"`
	AllowedOrigins       []string `json:"allowed_origins"`
	ReadHeaderTimeout    Duration `json:"read_header_timeout"`
	RequestTimeout       Duration `json:"request_timeout"`
	StreamWriteTimeout   Duration `json:"stream_write_timeout"`
	MaxBodyBytes         int64    `json:"max_body_bytes"`
	MaxSSEConnections    int      `json:"max_sse_connections"`
	RequestsPerMinute    int      `json:"requests_per_minute"`
}
type Auth struct {
	SessionIdle            Duration `json:"session_idle"`
	SessionLifetime        Duration `json:"session_lifetime"`
	VerificationLifetime   Duration `json:"verification_lifetime"`
	LoginAttemptsPerMinute int      `json:"login_attempts_per_minute"`
}
type Network struct {
	// Deployment-only allowlist, never writable through resource APIs.
	AllowedPrivateIPs []string `json:"allowed_private_ips"`
}

type Pricing struct {
	RulesFile         string `json:"rules_file"`
	RulesURL          string `json:"rules_url"`
	ExchangeRatesFile string `json:"exchange_rates_file"`
}

type Diagnostics struct {
	ModelResponses bool `json:"model_responses"`
}

type Debug struct {
	Enabled         bool `json:"enabled"`
	MaxRuns         int  `json:"max_runs"`
	MaxPayloadBytes int  `json:"max_payload_bytes"`
}

type ModelRetry struct {
	MaxAttempts int      `json:"max_attempts"`
	BaseDelay   Duration `json:"base_delay"`
	MaxDelay    Duration `json:"max_delay"`
	MaxWait     Duration `json:"max_wait"`
	Deadline    Duration `json:"deadline"`
}
type Execution struct {
	ModelRetry      ModelRetry `json:"model_retry"`
	MaxAssistants   int        `json:"max_assistants"`
	MaxModelCalls   int        `json:"max_model_calls"`
	MaxToolCalls    int        `json:"max_tool_calls"`
	MaxIndexJobs    int        `json:"max_index_jobs"`
	MaxUploadBytes  int64      `json:"max_upload_bytes"`
	MaxDiskBytes    int64      `json:"max_disk_bytes"`
	ShutdownTimeout Duration   `json:"shutdown_timeout"`
}

type Updates struct {
	Source    string `json:"source"`
	AutoCheck bool   `json:"auto_check"`
}

// Config contains no raw password, model key or bearer token. PublicURL must
// be HTTPS except in explicit loopback-only local mode. Validation checks shape;
// startup must verify certificates, proxy reachability and data permissions.
type Telegram struct {
	ProxyURL string `json:"proxy_url"`
}

type Config struct {
	Logging     logging.Options `json:"logging"`
	Telegram    Telegram        `json:"telegram"`
	ConfigPath  string          `json:"-"`
	Updates     Updates         `json:"updates"`
	PortForward PortForward     `json:"port_forward"`
	Browser     Browser         `json:"browser"`
	Sandbox     Sandbox         `json:"sandbox"`
	DataDir     string          `json:"data_dir"`
	HTTP        HTTP            `json:"http"`
	Auth        Auth            `json:"auth"`
	Execution   Execution       `json:"execution"`
	Network     Network         `json:"network"`
	Pricing     Pricing         `json:"pricing"`
	Debug       Debug           `json:"debug"`
	Diagnostics Diagnostics     `json:"diagnostics"`
}

func Defaults(dataDir string) Config {
	return Config{DataDir: dataDir,
		Logging:     logging.DefaultOptions(),
		Updates:     Updates{AutoCheck: true},
		PortForward: PortForward{Enabled: true, ListenHost: "0.0.0.0"},
		Sandbox:     sandboxDefaults(),
		Debug:       Debug{MaxRuns: 1000, MaxPayloadBytes: 4 << 20},
		Network:     Network{AllowedPrivateIPs: []string{"127.0.0.1"}},
		HTTP:        HTTP{Listen: "0.0.0.0:8080", TLSMode: DirectTLS, ReadHeaderTimeout: Duration(10 * time.Second), RequestTimeout: Duration(30 * time.Second), StreamWriteTimeout: Duration(10 * time.Second), MaxBodyBytes: 1 << 20, MaxSSEConnections: 20, RequestsPerMinute: 600},
		Auth:        Auth{SessionIdle: Duration(7 * 24 * time.Hour), SessionLifetime: Duration(30 * 24 * time.Hour), VerificationLifetime: Duration(5 * time.Minute), LoginAttemptsPerMinute: 5},
		Execution:   Execution{ModelRetry: ModelRetry{MaxAttempts: 4, BaseDelay: Duration(2 * time.Second), MaxDelay: Duration(30 * time.Second), MaxWait: Duration(120 * time.Second), Deadline: Duration(10 * time.Minute)}, MaxAssistants: 4, MaxModelCalls: 4, MaxToolCalls: 4, MaxIndexJobs: 1, MaxUploadBytes: 20 << 20, MaxDiskBytes: 10 << 30, ShutdownTimeout: Duration(30 * time.Second)},
	}
}

// Load overlays strict JSON onto defaults and rejects trailing/unknown content.
// Configuration input is bounded to avoid uncontrolled allocation at startup.
func Load(r io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return Config{}, core.Wrap(core.InvalidArgument, "cannot read configuration", err)
	}
	if len(data) > 1<<20 {
		return Config{}, core.Invalid("config", "configuration too large")
	}
	if trimmed := strings.TrimSpace(string(data)); !strings.HasPrefix(trimmed, "{") {
		return Config{}, core.Invalid("config", "object required")
	}
	c := Defaults("")
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Config{}, core.Wrap(core.InvalidArgument, "invalid configuration", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, core.Invalid("config", "trailing content")
	}
	return c, c.Validate()
}

// OwnListenPort returns this service's HTTP listen port, or 0 when the listen
// address is not parseable. Network presets use it to fence the service's own
// API away from broad host grants and to scope the molr preset to that port.
func (c Config) OwnListenPort() uint16 {
	_, port, err := net.SplitHostPort(c.HTTP.Listen)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return 0
	}
	return uint16(n)
}

func (c Config) Validate() error {
	if err := c.Logging.Validate(); err != nil {
		return core.Invalid("logging", err.Error())
	}
	if c.Telegram.ProxyURL != "" {
		u, err := url.Parse(c.Telegram.ProxyURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return core.Invalid("telegram.proxy_url", "HTTP(S) proxy URL without credentials, path, query or fragment required")
		}
	}

	if c.Updates.Source != "" {
		u, err := url.Parse(c.Updates.Source)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return core.Invalid("updates.source", "HTTP(S) source without credentials, query or fragment required")
		}
		if u.Scheme == "http" {
			ip, err := netip.ParseAddr(u.Hostname())
			if err != nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
				return core.Invalid("updates.source", "plain HTTP requires a private numeric address")
			}
		}
	}

	if err := c.PortForward.validate(); err != nil {
		return err
	}
	retry := c.Execution.ModelRetry
	if retry.MaxAttempts < 1 || retry.MaxAttempts > 10 || retry.BaseDelay < Duration(time.Millisecond) || retry.MaxDelay < retry.BaseDelay || retry.MaxDelay > Duration(time.Minute) || retry.MaxWait <= 0 || retry.MaxWait > Duration(10*time.Minute) || retry.Deadline < Duration(time.Second) || retry.Deadline > Duration(time.Hour) {
		return core.Invalid("execution.model_retry", "invalid retry limits")
	}
	if err := c.Sandbox.validate(c.DataDir, c.OwnListenPort()); err != nil {
		return err
	}
	if c.Debug.MaxRuns < 1 || c.Debug.MaxRuns > 10000 || c.Debug.MaxPayloadBytes < 1024 || c.Debug.MaxPayloadBytes > 8<<20 {
		return core.Invalid("debug", "invalid capture limits")
	}
	if c.Pricing.ExchangeRatesFile != "" && !filepath.IsAbs(c.Pricing.ExchangeRatesFile) {
		return core.Invalid("pricing.exchange_rates_file", "absolute rates file path required")
	}
	if c.Pricing.RulesFile != "" && !filepath.IsAbs(c.Pricing.RulesFile) {
		return core.Invalid("pricing.rules_file", "absolute rules file path required")
	}
	if c.Pricing.RulesURL != "" {
		u, err := url.Parse(c.Pricing.RulesURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return core.Invalid("pricing.rules_url", "HTTPS URL without credentials or fragment required")
		}
		if c.Pricing.RulesFile != "" {
			return core.Invalid("pricing", "rules_file and rules_url are mutually exclusive")
		}
	}
	if !filepath.IsAbs(c.DataDir) || filepath.Dir(filepath.Clean(c.DataDir)) == filepath.Clean(c.DataDir) {
		return core.Invalid("data_dir", "dedicated absolute directory required")
	}
	host, port, err := net.SplitHostPort(c.HTTP.Listen)
	number, numberErr := strconv.Atoi(port)
	if err != nil || numberErr != nil || number < 1 || number > 65535 {
		return core.Invalid("listen", "host and numeric port between 1 and 65535 required")
	}
	u, err := url.Parse(c.HTTP.PublicURL)
	if err != nil || !validHTTPScheme(u, c.HTTP.TLSMode) || u.Hostname() == "" || strings.Contains(u.Host, "*") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return core.Invalid("public_url", "HTTPS origin required")
	}
	for _, origin := range c.HTTP.AllowedOrigins {
		o, err := url.Parse(origin)
		if err != nil || !validHTTPScheme(o, c.HTTP.TLSMode) || o.Hostname() == "" || o.User != nil || o.RawQuery != "" || o.Fragment != "" || o.Path != "" || strings.Contains(o.Host, "*") {
			return core.Invalid("allowed_origins", "explicit HTTPS origins required")
		}
	}
	if c.HTTP.AutoRenewCertificate && c.HTTP.TLSMode != DirectTLS {
		return core.Invalid("auto_renew_certificate", "direct TLS required")
	}
	switch c.HTTP.TLSMode {
	case LocalHTTP:
		address, err := netip.ParseAddr(host)
		if err != nil || !address.IsLoopback() || address.Zone() != "" || u.Scheme != "http" || !loopbackHost(u.Hostname()) || c.HTTP.CertFile != "" || c.HTTP.KeyFile != "" || len(c.HTTP.TrustedProxies) != 0 {
			return core.Invalid("local_http", "numeric loopback listener, local HTTP URL and no TLS/proxy settings required")
		}
		urlPort := u.Port()
		if urlPort == "" {
			urlPort = "80"
		}
		if urlPort != port {
			return core.Invalid("public_url", "local URL must use the listening port")
		}
	case LANHTTP:
		address, err := netip.ParseAddr(host)
		if err != nil || address.Zone() != "" || !(address.IsUnspecified() || PrivateHTTPHost(host)) || u.Scheme != "http" || !PrivateHTTPHost(u.Hostname()) || c.HTTP.CertFile != "" || c.HTTP.KeyFile != "" || len(c.HTTP.TrustedProxies) != 0 {
			return core.Invalid("lan_http", "private/loopback HTTP URL, numeric listener and no TLS/proxy settings required")
		}
		if u.Port() != port && !(u.Port() == "" && port == "80") {
			return core.Invalid("public_url", "LAN URL must use the listening port")
		}
	case DirectTLS:
		if c.HTTP.CertFile == "" || c.HTTP.KeyFile == "" || len(c.HTTP.TrustedProxies) > 0 {
			return core.Invalid("tls", "certificate pair and no trusted proxies required")
		}
	case TrustedProxy:
		if len(c.HTTP.TrustedProxies) == 0 || c.HTTP.CertFile != "" || c.HTTP.KeyFile != "" {
			return core.Invalid("tls", "explicit proxy networks required")
		}
		for _, network := range c.HTTP.TrustedProxies {
			prefix, err := netip.ParsePrefix(network)
			if err != nil || prefix.Bits() == 0 || prefix != prefix.Masked() {
				return core.Invalid("trusted_proxies", "specific canonical network required")
			}
		}
	default:
		return core.Invalid("tls_mode", "direct, proxy, local or lan required")
	}
	if c.HTTP.RequestsPerMinute <= 0 {
		return core.Invalid("http.requests_per_minute", "positive limit required")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 || c.HTTP.RequestTimeout <= 0 || c.HTTP.StreamWriteTimeout <= 0 || c.HTTP.MaxBodyBytes <= 0 || c.HTTP.MaxSSEConnections <= 0 {
		return core.Invalid("http", "positive limits required")
	}
	if c.Auth.SessionIdle <= 0 || c.Auth.SessionLifetime < c.Auth.SessionIdle || c.Auth.VerificationLifetime <= 0 || c.Auth.VerificationLifetime > c.Auth.SessionIdle || c.Auth.LoginAttemptsPerMinute <= 0 {
		return core.Invalid("auth", "invalid session or login limits")
	}
	for _, raw := range c.Network.AllowedPrivateIPs {
		address, err := netip.ParseAddr(raw)
		address = address.Unmap()
		if err != nil || address.Zone() != "" || (!address.IsPrivate() && !address.IsLoopback() && !netip.MustParsePrefix("100.64.0.0/10").Contains(address)) || address.IsLinkLocalUnicast() || address == netip.MustParseAddr("100.100.100.200") || address == netip.MustParseAddr("fd00:ec2::254") {
			return core.Invalid("network.allowed_private_ips", "explicit private or loopback IP required; metadata addresses prohibited")
		}
	}
	e := c.Execution
	if e.MaxAssistants <= 0 || e.MaxModelCalls <= 0 || e.MaxToolCalls <= 0 || e.MaxIndexJobs <= 0 || e.MaxUploadBytes <= 0 || e.MaxDiskBytes < e.MaxUploadBytes || e.ShutdownTimeout <= 0 {
		return core.Invalid("execution", "positive bounded execution limits required")
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback() && ip.Zone() == ""
}
func validScheme(u *url.URL, local bool) bool {
	if local {
		return (u.Scheme == "http" || u.Scheme == "https") && loopbackHost(u.Hostname())
	}
	return u.Scheme == "https"
}

// PlainHTTP keeps transport and cookie decisions consistent.
func (h HTTP) PlainHTTP() bool { return h.TLSMode == LocalHTTP || h.TLSMode == LANHTTP }
func PrivateHTTPHost(host string) bool {
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Zone() == "" && (ip.Unmap().IsPrivate() || ip.Unmap().IsLoopback())
}
func validHTTPScheme(u *url.URL, mode TLSMode) bool {
	if mode == LANHTTP {
		return u.Scheme == "http" && PrivateHTTPHost(u.Hostname())
	}
	return validScheme(u, mode == LocalHTTP)
}
