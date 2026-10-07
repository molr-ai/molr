package platform

import (
	"net/http"
	"net/http/httptest"
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
