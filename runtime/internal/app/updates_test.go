package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/updates"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpdateAPIUsesSharedConfiguredSourceAndRequiresCSRF(t *testing.T) {
	var calls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(updates.Release{Version: "0.2.0", Commit: strings.Repeat("a", 40), PublishedAt: time.Now(), NotesURL: "https://example.com/releases/0.2.0", File: "molr_0.2.0_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz", SHA256: strings.Repeat("b", 64)})
	}))
	defer source.Close()
	cfg := config.Defaults(t.TempDir())
	cfg.HTTP.TLSMode = config.LocalHTTP
	cfg.HTTP.Listen = "127.0.0.1:8787"
	cfg.HTTP.PublicURL = "http://127.0.0.1:8787"
	cfg.Updates.Source = source.URL
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	ownerSession(t, b)
	request := func(method, path, body string, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, cfg.HTTP.PublicURL+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:4321"
		r.Header.Set("Origin", cfg.HTTP.PublicURL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		b.HTTP.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/updates", "", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request("GET", "/api/auth/csrf", "", nil, "")
	var proof struct {
		Token string `json:"csrf_token"`
	}
	need(t, json.Unmarshal(w.Body.Bytes(), &proof))
	w = request("POST", "/api/auth/login", `{"username":"owner","password":"correct-horse-battery-staple-2026!"}`, w.Result().Cookies(), proof.Token)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	need(t, json.Unmarshal(w.Body.Bytes(), &proof))
	if w = request("GET", "/api/updates", "", cookies, ""); w.Code != 200 || calls.Load() != 0 {
		t.Fatal(w.Code, calls.Load())
	}
	if w = request("POST", "/api/updates/check", `{}`, cookies, ""); w.Code != 403 || calls.Load() != 0 {
		t.Fatal("CSRF bypass")
	}
	for range 2 {
		w = request("POST", "/api/updates/check", `{"automatic":false}`, cookies, proof.Token)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var status updates.Snapshot
	need(t, json.Unmarshal(w.Body.Bytes(), &status))
	if status.Latest == nil || status.Latest.Version != "0.2.0" || calls.Load() != 1 {
		t.Fatal(status, calls.Load())
	}
	w = request("POST", "/api/updates/check", `{"url":"http://127.0.0.1/other"}`, cookies, proof.Token)
	if w.Code != 400 {
		t.Fatal("arbitrary source accepted", w.Code)
	}
}
