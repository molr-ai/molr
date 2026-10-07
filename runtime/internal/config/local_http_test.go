package config

import "testing"

func TestLocalHTTPRequiresExplicitLoopbackConfiguration(t *testing.T) {
	base := Defaults(t.TempDir())
	base.HTTP.TLSMode = TLSMode("local")
	base.HTTP.Listen = "127.0.0.1:8080"
	base.HTTP.PublicURL = "http://127.0.0.1:8080"
	base.HTTP.AllowedOrigins = []string{"http://127.0.0.1:5188", "http://localhost:5188"}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	ipv6 := base
	ipv6.HTTP.Listen = "[::1]:8080"
	ipv6.HTTP.PublicURL = "http://[::1]:8080"
	if err := ipv6.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"zero request rate":     func(c *Config) { c.HTTP.RequestsPerMinute = 0 },
		"negative request rate": func(c *Config) { c.HTTP.RequestsPerMinute = -1 },
		"wildcard":              func(c *Config) { c.HTTP.Listen = "0.0.0.0:8080" },
		"ipv6 wildcard":         func(c *Config) { c.HTTP.Listen = "[::]:8080" },
		"LAN":                   func(c *Config) { c.HTTP.Listen = "192.168.1.10:8080" },
		"DNS listener":          func(c *Config) { c.HTTP.Listen = "localhost:8080" },
		"public URL":            func(c *Config) { c.HTTP.PublicURL = "http://example.com:8080" },
		"LAN URL":               func(c *Config) { c.HTTP.PublicURL = "http://192.168.1.10:8080" },
		"proxy":                 func(c *Config) { c.HTTP.TrustedProxies = []string{"127.0.0.1/32"} },
		"certificate":           func(c *Config) { c.HTTP.CertFile = "cert.pem" },
		"remote origin":         func(c *Config) { c.HTTP.AllowedOrigins = []string{"https://example.com"} },
		"LAN origin":            func(c *Config) { c.HTTP.AllowedOrigins = []string{"http://192.168.1.10:5188"} },
		"URL port":              func(c *Config) { c.HTTP.PublicURL = "http://127.0.0.1:8081" },
		"implicit plaintext":    func(c *Config) { c.HTTP.TLSMode = DirectTLS; c.HTTP.CertFile = "cert"; c.HTTP.KeyFile = "key" },
	} {
		t.Run(name, func(t *testing.T) {
			c := base
			change(&c)
			if c.Validate() == nil {
				t.Fatal("unsafe or ambiguous local HTTP configuration accepted")
			}
		})
	}
}
