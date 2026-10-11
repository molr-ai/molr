package platform

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestPublicHTTPClientRejectsPrivateAndPlainHTTP(t *testing.T) {
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer server.Close()
	client := PublicHTTPClient(server.URL)
	defer client.CloseIdleConnections()
	if _, err := client.Get(server.URL); err == nil || hit {
		t.Fatal("fixed integration reached private HTTP")
	}
}

func TestConfiguredHTTPClientPinsExplicitLocalSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("manifest")) }))
	defer server.Close()
	client := ConfiguredHTTPClient(server.URL, []netip.Addr{netip.MustParseAddr("127.0.0.1")})
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL, 302) }))
	defer redirect.Close()
	client = ConfiguredHTTPClient(redirect.URL, []netip.Addr{netip.MustParseAddr("127.0.0.1")})
	if response, err := client.Get(redirect.URL); err == nil {
		response.Body.Close()
		t.Fatal("redirect accepted")
	}
}
