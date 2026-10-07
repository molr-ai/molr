package config

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
)

func TestTDDConfigDefaultAndDirectTLS(t *testing.T) {
	c := Defaults("/var/lib/molr")
	if c.HTTP.Listen != "0.0.0.0:8080" {
		t.Fatal("default must support LAN/public deployment", c.HTTP.Listen)
	}
	if c.Validate() == nil {
		t.Fatal("defaults unexpectedly open an unconfigured public service")
	}
	c.HTTP.PublicURL = "https://molr.example.test"
	c.HTTP.CertFile = "/etc/molr/cert.pem"
	c.HTTP.KeyFile = "/etc/molr/key.pem"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.HTTP.TrustedProxies = []string{"192.0.2.0/24"}
	if c.Validate() == nil {
		t.Fatal("ambiguous direct/proxy configuration accepted")
	}
}

func TestTDDConfigRejectsUnboundedOrAmbiguousConfiguration(t *testing.T) {
	changes := map[string]func(*Config){
		"root data":           func(c *Config) { c.DataDir = "/" },
		"empty data":          func(c *Config) { c.DataDir = "" },
		"zero port":           func(c *Config) { c.HTTP.Listen = "0.0.0.0:0" },
		"oversized port":      func(c *Config) { c.HTTP.Listen = "0.0.0.0:65536" },
		"missing port":        func(c *Config) { c.HTTP.Listen = "0.0.0.0" },
		"public query":        func(c *Config) { c.HTTP.PublicURL = "https://host.test/?token=x" },
		"public path":         func(c *Config) { c.HTTP.PublicURL = "https://host.test/private" },
		"public fragment":     func(c *Config) { c.HTTP.PublicURL = "https://host.test/#token" },
		"invalid mode":        func(c *Config) { c.HTTP.TLSMode = TLSMode("off") },
		"IPv6 world proxy":    func(c *Config) { c.HTTP.TrustedProxies = []string{"::/0"} },
		"noncanonical proxy":  func(c *Config) { c.HTTP.TrustedProxies = []string{"192.0.2.1/24"} },
		"proxy with key":      func(c *Config) { c.HTTP.KeyFile = "/etc/key" },
		"origin credentials":  func(c *Config) { c.HTTP.AllowedOrigins = []string{"https://u:p@host.test"} },
		"origin path":         func(c *Config) { c.HTTP.AllowedOrigins = []string{"https://host.test/path"} },
		"zero idle":           func(c *Config) { c.Auth.SessionIdle = 0 },
		"long verification":   func(c *Config) { c.Auth.VerificationLifetime = c.Auth.SessionIdle + 1 },
		"no login limit":      func(c *Config) { c.Auth.LoginAttemptsPerMinute = 0 },
		"no stream limit":     func(c *Config) { c.HTTP.MaxSSEConnections = 0 },
		"no header timeout":   func(c *Config) { c.HTTP.ReadHeaderTimeout = 0 },
		"no request timeout":  func(c *Config) { c.HTTP.RequestTimeout = 0 },
		"no stream timeout":   func(c *Config) { c.HTTP.StreamWriteTimeout = 0 },
		"no assistant limit":  func(c *Config) { c.Execution.MaxAssistants = 0 },
		"no tool limit":       func(c *Config) { c.Execution.MaxToolCalls = 0 },
		"no index limit":      func(c *Config) { c.Execution.MaxIndexJobs = 0 },
		"no upload limit":     func(c *Config) { c.Execution.MaxUploadBytes = 0 },
		"quota below upload":  func(c *Config) { c.Execution.MaxDiskBytes = c.Execution.MaxUploadBytes - 1 },
		"no shutdown timeout": func(c *Config) { c.Execution.ShutdownTimeout = 0 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			change(&c)
			if core.CodeOf(c.Validate()) != core.InvalidArgument {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

type failingConfigReader struct{}

func (failingConfigReader) Read([]byte) (int, error) { return 0, errors.New("private path unreadable") }

func TestTDDConfigLoadStrictAndBounded(t *testing.T) {
	minimum := `{"data_dir":"/var/lib/molr","http":{"public_url":"https://host.test","tls_mode":"proxy","trusted_proxies":["192.0.2.1/32"]}}`
	c, err := Load(strings.NewReader(minimum))
	if err != nil {
		t.Fatal(err)
	}
	if c.Execution.MaxAssistants != 4 || c.HTTP.Listen != "0.0.0.0:8080" {
		t.Fatal("defaults not preserved")
	}
	for _, value := range []string{`[]`, `null`, minimum + ` true`, strings.Replace(minimum, `"http":{`, `"http":{"unknown_setting":true,`, 1), strings.Repeat(" ", 1<<20) + minimum} {
		if _, err := Load(strings.NewReader(value)); core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("accepted %q", value[:min(len(value), 100)])
		}
	}
	if _, err := Load(failingConfigReader{}); core.CodeOf(err) != core.InvalidArgument || strings.Contains(err.Error(), "private path") {
		t.Fatal("reader failure leaked or misclassified", err)
	}
	if _, err := Load(io.LimitReader(strings.NewReader(minimum), 10)); err == nil {
		t.Fatal("truncated configuration accepted")
	}
}

func TestTDDConfigDurationRoundTripAndInvalidInput(t *testing.T) {
	d := Duration(1500 * time.Millisecond)
	encoded, err := d.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var actual Duration
	if err := actual.UnmarshalJSON(encoded); err != nil {
		t.Fatal(err)
	}
	if actual.Value() != 1500*time.Millisecond {
		t.Fatal(actual)
	}
	for _, input := range []string{`123`, `null`, `"not-time"`, `{}`} {
		if err := json.Unmarshal([]byte(input), &actual); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal("invalid duration accepted", input, err)
		}
	}
}
