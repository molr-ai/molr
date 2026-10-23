package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	c := Defaults("/var/lib/molr")
	c.HTTP.PublicURL = "https://molr.example.com"
	c.HTTP.TLSMode = TrustedProxy
	c.HTTP.TrustedProxies = []string{"192.0.2.10/32"}
	return c
}
func TestPublicConfigFailsClosed(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	changes := map[string]func(*Config){
		"plaintext":            func(c *Config) { c.HTTP.PublicURL = "http://molr.example.com" },
		"credentials":          func(c *Config) { c.HTTP.PublicURL = "https://user:pass@molr.example.com" },
		"wildcard public host": func(c *Config) { c.HTTP.PublicURL = "https://*.example.com" },
		"no proxy allowlist":   func(c *Config) { c.HTTP.TrustedProxies = nil },
		"world proxy":          func(c *Config) { c.HTTP.TrustedProxies = []string{"0.0.0.0/0"} },
		"wildcard origin":      func(c *Config) { c.HTTP.AllowedOrigins = []string{"https://*.example.com"} },
		"bad port":             func(c *Config) { c.HTTP.Listen = "0.0.0.0:invalid" },
		"relative data":        func(c *Config) { c.DataDir = "." },
		"no request bound":     func(c *Config) { c.HTTP.MaxBodyBytes = 0 },
		"unbounded execution":  func(c *Config) { c.Execution.MaxModelCalls = 0 },
		"invalid expiry":       func(c *Config) { c.Auth.SessionLifetime = c.Auth.SessionIdle - 1 },
		"missing direct cert":  func(c *Config) { c.HTTP.TLSMode = DirectTLS; c.HTTP.TrustedProxies = nil },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			change(&c)
			if c.Validate() == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}
func TestLoadStrictConfiguration(t *testing.T) {
	data, err := json.Marshal(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HTTP.ReadHeaderTimeout != Duration(10*time.Second) {
		t.Fatal("duration did not round trip")
	}
	for _, input := range []string{`null`, string(data) + ` {}`, `{"unknown":true}`, strings.Repeat("x", (1<<20)+1)} {
		if _, err := Load(strings.NewReader(input)); err == nil {
			t.Fatal("invalid JSON configuration accepted")
		}
	}
}

func TestPricingSourcesAreOptionalAndMutuallyExclusive(t *testing.T) {
	c := validConfig()
	if c.Pricing.RulesFile != "" || c.Pricing.RulesURL != "" {
		t.Fatal("pricing source enabled by default")
	}
	for _, source := range []Pricing{{}, {RulesFile: "/etc/molr/pricing.json"}, {RulesURL: "https://prices.example/models.json"}} {
		c.Pricing = source
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(strings.NewReader(string(data)))
		if err != nil || loaded.Pricing != source {
			t.Fatal("pricing source did not round trip", err)
		}
	}
	for _, source := range []Pricing{{RulesFile: "relative.json"}, {RulesURL: "http://prices.example/models.json"}, {RulesURL: "https://user:secret@prices.example/models.json"}, {RulesURL: "https://prices.example/models.json#fragment"}, {RulesURL: "https:///models.json"}, {RulesFile: "/etc/molr/pricing.json", RulesURL: "https://prices.example/models.json"}} {
		c.Pricing = source
		if c.Validate() == nil {
			t.Fatal("invalid pricing source accepted")
		}
	}
}

func TestNetworkAllowlistIsExplicitDeploymentConfiguration(t *testing.T) {
	c := validConfig()
	if len(c.Network.AllowedPrivateIPs) != 1 || c.Network.AllowedPrivateIPs[0] != "127.0.0.1" {
		t.Fatal("default network must allow only IPv4 loopback")
	}
	c.Network.AllowedPrivateIPs = []string{"127.0.0.1", "::1", "192.168.1.20", "fd00::20", "100.64.1.2"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(strings.NewReader(string(encoded)))
	if err != nil || len(loaded.Network.AllowedPrivateIPs) != 5 {
		t.Fatalf("network config round trip: %v", err)
	}
	loaded, err = Load(strings.NewReader(strings.Replace(string(encoded), `"allowed_private_ips":["127.0.0.1","::1","192.168.1.20","fd00::20","100.64.1.2"]`, `"allowed_private_ips":[]`, 1)))
	if err != nil || len(loaded.Network.AllowedPrivateIPs) != 0 {
		t.Fatal("explicit empty allowlist must disable loopback access")
	}
	for _, value := range []string{"*", "192.168.0.0/16", "localhost", "0.0.0.0", "::", "8.8.8.8", "169.254.169.254", "100.100.100.200", "fd00:ec2::254", "fe80::1%eth0"} {
		c.Network.AllowedPrivateIPs = []string{value}
		if c.Validate() == nil {
			t.Fatalf("invalid deployment allowlist accepted: %s", value)
		}
	}
}
