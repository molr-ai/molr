package config

import "testing"

func TestTelegramProxyIsDeploymentOnlyOrigin(t *testing.T) {
	for _, proxy := range []string{"", "http://127.0.0.1:3128", "https://proxy.example.com:8443"} {
		c := validConfig()
		c.Telegram.ProxyURL = proxy
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, proxy := range []string{"file:///tmp/proxy", "http://user:password@proxy.example.com", "http://proxy.example.com/path", "http://proxy.example.com?token=test", "http://proxy.example.com#fragment"} {
		c := validConfig()
		c.Telegram.ProxyURL = proxy
		if c.Validate() == nil {
			t.Fatalf("accepted %s", proxy)
		}
	}
}
