package platform_test

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfiguredServiceHTTPClientKeepsDialPolicy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://100.100.100.200/", http.StatusFound)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	client := platform.ServiceHTTPClient(server.URL)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 || requests != 1 {
		t.Fatal(response.StatusCode, requests)
	}
	response, err = client.Get(server.URL + "/redirect")
	if core.CodeOf(err) != core.Forbidden || requests != 2 {
		t.Fatal("redirect was not rejected", err, requests)
	}
	if response != nil {
		response.Body.Close()
	}
	_, err = client.Get("http://100.100.100.200/")
	if err == nil {
		t.Fatal("origin changed")
	}
	_, err = platform.ServiceHTTPClient("http://100.100.100.200").Get("http://100.100.100.200/")
	if core.ReasonOf(err) != core.NetworkDestinationProhibited {
		t.Fatal("metadata allowed", err)
	}
}
