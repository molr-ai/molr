package testkit

import (
	"io"
	"net/http"
	"testing"
)

// DrainRequestBody consumes an unused request body before a test server replies.
// Otherwise a non-keep-alive connection can close while the client is still
// uploading, turning the scripted response into an unrelated transport failure.
// Handlers inspecting the payload should read it themselves. Do not use this
// for tests that intentionally interrupt an upload or disconnect the client.
func DrainRequestBody(t *testing.T, r *http.Request) bool {
	t.Helper()
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		t.Errorf("read test request body: %v", err)
		return false
	}
	return true
}
