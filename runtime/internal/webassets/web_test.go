package webassets

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebUIRouting(t *testing.T) {
	for _, tc := range []struct {
		path   string
		status int
	}{{"/", 200}, {"/assistants", 200}, {"/missing.js", 404}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
		if tc.status == 200 && !strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>") {
			t.Fatal("missing document")
		}
	}
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}
