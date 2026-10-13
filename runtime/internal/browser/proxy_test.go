package browser

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestProxyValidationAndPrivateLaunchConfig(t *testing.T) {
	for _, server := range []string{"http://host.molr.internal:7890", "socks5://[::1]:1080"} {
		p := Proxy{ProxySummary: ProxySummary{Mode: "proxy", Server: server, Bypass: []string{"*.example.test", "192.0.2.0/24"}}}
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, server := range []string{"https://proxy.test:443", "http://u:p@proxy.test:80", "http://proxy.test:80/path", "http://proxy.test:80?", "http://proxy.test", "http://proxy.test:0", "http://proxy.test:65536"} {
		p := Proxy{ProxySummary: ProxySummary{Mode: "proxy", Server: server}}
		if p.Validate() == nil {
			t.Fatal("accepted", server)
		}
	}
	p := Proxy{ProxySummary: ProxySummary{Mode: "proxy", Server: "http://proxy.test:8080"}, Username: "proxy-user", Password: "proxy-secret:@/%"}
	raw, err := p.LaunchConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "proxy-user:proxy-secret%3A%40%2F%25@") {
		t.Fatal("credentials not URL encoded")
	}
	visible, _ := json.Marshal(p)
	if strings.Contains(string(visible), p.Username) || strings.Contains(string(visible), "proxy-secret") {
		t.Fatal("secret in public JSON")
	}
	p.Server = "socks5://proxy.test:1080"
	if p.Validate() == nil {
		t.Fatal("SOCKS authentication accepted")
	}
	for _, bypass := range []string{"<-loopback>", "example.test;*", "http://example.test", "x\ny", "u:p@host"} {
		p := Proxy{ProxySummary: ProxySummary{Mode: "proxy", Server: "http://proxy.test:80", Bypass: []string{bypass}}}
		if p.Validate() == nil {
			t.Fatal("accepted bypass", bypass)
		}
	}
}

func TestProxyErrorRedactsAllCredentialForms(t *testing.T) {
	password := `fixture:"@/%密碼`
	username := "fixture-user"
	server := url.URL{Scheme: "http", Host: "proxy.test:8080", User: url.UserPassword(username, password)}
	encoded, _ := json.Marshal(password)
	for _, value := range []string{server.String(), server.User.String(), password, url.QueryEscape(password), url.PathEscape(password), string(encoded[1 : len(encoded)-1]), base64.StdEncoding.EncodeToString([]byte(username + ":" + password))} {
		got := RedactProxyError("failed "+value, server.String())
		if strings.Contains(got, value) || !strings.Contains(got, "[redacted]") {
			t.Fatal("credential not redacted")
		}
	}
	if got := RedactProxyError("net::ERR_PROXY_CONNECTION_FAILED", server.String()); got != "net::ERR_PROXY_CONNECTION_FAILED" {
		t.Fatal(got)
	}
}
