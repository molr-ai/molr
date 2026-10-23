package pricing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func updateRules(version string, input int) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":1,"version":%q,"currency":"USD","providers":[{"id":"fixture","origins":["https://provider.example"],"models":[{"id":"test","rates":{"input_micros_per_million":%d,"output_micros_per_million":20,"cached_input_micros_per_million":0}}]}]}`, version, input))
}

func newUpdateCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func requireUpdatedPrice(t *testing.T, c *Catalog, version string, input int64) {
	t.Helper()
	q, ok := c.Lookup("fixture", "https://provider.example", "test", time.Now())
	if !ok || q.Version != version || q.Current.Input != input {
		t.Fatalf("wrong active price: %+v", q)
	}
}

func TestPricingFileRefreshCacheAndRecovery(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	file := filepath.Join(dir, "source.json")
	opts := UpdateOptions{File: file, CacheDir: filepath.Join(dir, "cache")}
	c := newUpdateCatalog(t)
	u, err := NewUpdater(ctx, c, opts)
	if err != nil {
		t.Fatal(err)
	}
	if c.current.Load().version == "v1" {
		t.Fatal("missing source invented a price")
	}
	if err := os.WriteFile(file, updateRules("v1", 10), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := u.Refresh(ctx)
	if err != nil || !r.Updated || r.Source != "file" || r.Version != "v1" {
		t.Fatal(r, err)
	}
	requireUpdatedPrice(t, c, "v1", 10)
	old, _ := c.Lookup("fixture", "https://provider.example", "test", time.Now())
	info, _ := os.Stat(file)
	// Identical length and timestamp must not hide a content change.
	if err := os.WriteFile(file, updateRules("v2", 11), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	requireUpdatedPrice(t, c, "v2", 11)
	if old.Version != "v1" || old.Current.Input != 10 {
		t.Fatal("old quote changed")
	}
	for _, invalid := range [][]byte{[]byte(`{}`), bytes.Repeat([]byte("x"), (1<<20)+1)} {
		if err := os.WriteFile(file, invalid, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := u.Refresh(ctx); err == nil {
			t.Fatal("invalid source accepted")
		}
		requireUpdatedPrice(t, c, "v2", 11)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	restarted := newUpdateCatalog(t)
	u2, err := NewUpdater(ctx, restarted, opts)
	if err != nil {
		t.Fatal(err)
	}
	requireUpdatedPrice(t, restarted, "v2", 11)
	// A new source never inherits the old source's cache.
	opts.File = filepath.Join(dir, "other.json")
	other := newUpdateCatalog(t)
	if _, err := NewUpdater(ctx, other, opts); err != nil {
		t.Fatal(err)
	}
	if other.current.Load().version == "v2" {
		t.Fatal("cache crossed source identities")
	}
	// Atomic replacement is picked up just like an in-place write.
	temporary := filepath.Join(dir, "next.json")
	if err := os.WriteFile(temporary, updateRules("v3", 12), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, file); err != nil {
		t.Fatal(err)
	}
	if _, err := u2.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	requireUpdatedPrice(t, restarted, "v3", 12)
	if r, err := u2.Refresh(ctx); err != nil || r.Updated {
		t.Fatal("unchanged file replaced", r, err)
	}
	if info, err := os.Stat(u2.cachePath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache permissions", info, err)
	}
}

func TestPricingURLConditionalRefreshAndFailureRetention(t *testing.T) {
	var mu sync.Mutex
	body, etag, status := updateRules("v1", 10), `"v1"`, 200
	var seenETag, seenModified string
	var requests int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		seenETag, seenModified = r.Header.Get("If-None-Match"), r.Header.Get("If-Modified-Since")
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") == "" {
			t.Error("missing request metadata")
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Fri, 23 Oct 2026 01:00:00 GMT")
		if status != 200 {
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	c := newUpdateCatalog(t)
	opts := UpdateOptions{URL: server.URL + "/pricing.json", CacheDir: t.TempDir(), Client: server.Client()}
	u, err := NewUpdater(t.Context(), c, opts)
	if err != nil {
		t.Fatal(err)
	}
	requireUpdatedPrice(t, c, "v1", 10)
	mu.Lock()
	status = 304
	mu.Unlock()
	if r, err := u.Refresh(t.Context()); err != nil || r.Updated || r.Source != "url" {
		t.Fatal(r, err)
	}
	mu.Lock()
	valid := seenETag == `"v1"` && seenModified != ""
	mu.Unlock()
	if !valid {
		t.Fatal("conditional validators missing")
	}
	mu.Lock()
	status, body, etag = 200, []byte(`{}`), `"invalid"`
	mu.Unlock()
	if _, err := u.Refresh(t.Context()); err == nil {
		t.Fatal("invalid JSON tariff accepted")
	}
	requireUpdatedPrice(t, c, "v1", 10)
	mu.Lock()
	body, etag = updateRules("v2", 12), `"v2"`
	mu.Unlock()
	if r, err := u.Refresh(t.Context()); err != nil || !r.Updated {
		t.Fatal(r, err)
	}
	mu.Lock()
	valid = seenETag == `"v1"`
	mu.Unlock()
	if !valid {
		t.Fatal("failed response poisoned ETag")
	}
	requireUpdatedPrice(t, c, "v2", 12)
	for _, code := range []int{302, 404, 500} {
		mu.Lock()
		status = code
		before := requests
		mu.Unlock()
		if _, err := u.Refresh(t.Context()); err == nil {
			t.Fatal("bad status accepted", code)
		}
		mu.Lock()
		after := requests
		mu.Unlock()
		if after != before+1 {
			t.Fatal("redirect followed")
		}
		requireUpdatedPrice(t, c, "v2", 12)
	}
	server.Close()
	restarted := newUpdateCatalog(t)
	if _, err := NewUpdater(t.Context(), restarted, opts); err != nil {
		t.Fatal(err)
	}
	requireUpdatedPrice(t, restarted, "v2", 12)
}

func TestPricingUpdaterDefaultsValidationAndCacheFailure(t *testing.T) {
	c := newUpdateCatalog(t)
	u, err := NewUpdater(t.Context(), c, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := u.Refresh(t.Context())
	if err != nil || r.Source != "builtin" || r.Updated {
		t.Fatal(r, err)
	}
	// Disabled polling returns without waiting for context cancellation.
	u.Run(t.Context())
	if refreshInterval != 24*time.Hour {
		t.Fatal("default interval changed")
	}
	for _, opts := range []UpdateOptions{{File: "relative"}, {URL: "http://pricing.example/rules"}, {URL: "https://user:pass@pricing.example/rules"}, {URL: "https://pricing.example/rules#fragment"}, {File: "/rules.json", URL: "https://pricing.example/rules"}, {File: "/rules.json", CacheDir: "relative"}} {
		if _, err := NewUpdater(t.Context(), c, opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(file, updateRules("v1", 10), 0600); err != nil {
		t.Fatal(err)
	}
	u, err = NewUpdater(t.Context(), c, UpdateOptions{File: file, CacheDir: filepath.Join(dir, "cache")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(u.options.CacheDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(u.options.CacheDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, updateRules("v2", 11), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Refresh(t.Context()); err == nil {
		t.Fatal("unpersisted price published")
	}
	requireUpdatedPrice(t, c, "v1", 10)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := u.Refresh(ctx); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestPricingUpdaterPollingAndConcurrentCancellation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(file, updateRules("v1", 10), 0600); err != nil {
		t.Fatal(err)
	}
	c := newUpdateCatalog(t)
	u, err := NewUpdater(t.Context(), c, UpdateOptions{File: file, CacheDir: filepath.Join(dir, "cache")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); u.run(ctx, 10*time.Millisecond) }()
	if err := os.WriteFile(file, updateRules("v2", 11), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for c.current.Load().version != "v2" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poller did not stop")
	}
	requireUpdatedPrice(t, c, "v2", 11)
	var blocked atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if blocked.Load() {
			close(entered)
			<-release
		}
		_, _ = w.Write(updateRules("remote", 13))
	}))
	defer server.Close()
	u, err = NewUpdater(t.Context(), c, UpdateOptions{URL: server.URL, CacheDir: t.TempDir(), Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	blocked.Store(true)
	first := make(chan error, 1)
	go func() { _, err := u.Refresh(t.Context()); first <- err }()
	<-entered
	waiter, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	if _, err := u.Refresh(waiter); err == nil {
		t.Fatal("waiting caller cancellation ignored")
	}
	if calls.Load() != 2 {
		t.Fatal("concurrent refresh dispatched twice")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestPricingURLRejectsOversizeAndUnexpectedNotModified(t *testing.T) {
	var oversize atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if oversize.Load() {
			_, _ = io.WriteString(w, strings.Repeat("x", (1<<20)+1))
			return
		}
		w.WriteHeader(304)
	}))
	defer server.Close()
	c := newUpdateCatalog(t)
	u, err := NewUpdater(t.Context(), c, UpdateOptions{URL: server.URL, CacheDir: t.TempDir(), Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Refresh(t.Context()); err == nil {
		t.Fatal("unconditional 304 accepted")
	}
	oversize.Store(true)
	if _, err := u.Refresh(t.Context()); err == nil {
		t.Fatal("unbounded remote content accepted")
	}
	if _, err := os.Stat(u.cachePath); !os.IsNotExist(err) {
		t.Fatal("invalid response persisted")
	}
}
