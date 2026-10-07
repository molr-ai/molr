package config

import "testing"

func TestLANHTTPConfigurationBoundaries(t *testing.T) {
	base := Defaults(t.TempDir())
	base.HTTP.TLSMode = LANHTTP
	base.HTTP.Listen = "0.0.0.0:8787"
	base.HTTP.PublicURL = "http://192.168.1.20:8787"
	base.HTTP.AllowedOrigins = []string{"http://127.0.0.1:8787"}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"public URL":      func(c *Config) { c.HTTP.PublicURL = "http://8.8.8.8:8787" },
		"DNS URL":         func(c *Config) { c.HTTP.PublicURL = "http://host.example:8787" },
		"public listener": func(c *Config) { c.HTTP.Listen = "8.8.8.8:8787" },
		"public origin":   func(c *Config) { c.HTTP.AllowedOrigins = []string{"http://evil.example"} },
		"proxy":           func(c *Config) { c.HTTP.TrustedProxies = []string{"127.0.0.1/32"} },
		"wrong port":      func(c *Config) { c.HTTP.PublicURL = "http://192.168.1.20:8788" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			change(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("unsafe LAN configuration accepted")
			}
		})
	}
}
