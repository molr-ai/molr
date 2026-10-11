package updates

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/buildinfo"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestVersionPrecedence(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		n    int
	}{{"0.9.0", "0.10.0", -1}, {"1.0.0-rc.2", "1.0.0-rc.10", -1}, {"1.0.0-rc.10", "1.0.0", -1}, {"1.0.0-a.1", "1.0.0-a.1.1", -1}, {"1.0.0-1", "1.0.0-a", -1}, {"1.0.0+one", "1.0.0+two", 0}, {"2.0.0", "1.99.99", 1}, {"1.0.0-99999999999999999999", "1.0.0-100000000000000000000", -1}} {
		n, ok := Compare(tc.a, tc.b)
		if !ok || n != tc.n {
			t.Fatalf("%s %s: %d %v", tc.a, tc.b, n, ok)
		}
	}
	for _, v := range []string{"1", "v1.0.0", "01.2.3", "1.2.3-01", "1.2.3-", "1.2.3+a..b"} {
		if _, ok := Compare(v, "1.0.0"); ok {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"development", "0.1.0-local", "0.1.0-dev.1"} {
		if !Development(v) {
			t.Fatal(v)
		}
	}
	if Development("0.1.0-rc.1") {
		t.Fatal("release candidate is comparable")
	}
}
func fixtureRelease() Release {
	return Release{Version: "0.2.0", Commit: strings.Repeat("a", 40), PublishedAt: time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC), NotesURL: "https://example.com/releases/0.2.0", File: "molr_0.2.0_linux_amd64.tar.gz", SHA256: strings.Repeat("b", 64)}
}
func identity() buildinfo.Info {
	return buildinfo.Info{Version: "0.1.0", Commit: strings.Repeat("c", 40), OS: "linux", Arch: "amd64"}
}
func TestCheckCoalescesAndRetainsSuccessfulResult(t *testing.T) {
	var hits atomic.Int32
	var fail atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest-linux-amd64.json" {
			t.Error(r.URL.Path)
		}
		if hits.Add(1) == 1 {
			close(entered)
			<-release
		}
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(fixtureRelease())
	}))
	defer server.Close()
	s := New(server.URL, true, identity(), server.Client())
	now := time.Now()
	s.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { s.Check(context.Background(), false) })
	}
	<-entered
	if !s.Snapshot().Check.Checking {
		t.Fatal("missing checking state")
	}
	close(release)
	wg.Wait()
	if hits.Load() != 1 || s.Snapshot().Comparison != "available" {
		t.Fatal(s.Snapshot(), hits.Load())
	}
	now = now.Add(2 * time.Minute)
	s.Check(context.Background(), true)
	if hits.Load() != 1 {
		t.Fatal("automatic cooldown ignored")
	}
	s.Check(context.Background(), false)
	if hits.Load() != 2 {
		t.Fatal("manual check blocked by automatic TTL")
	}
	fail.Store(true)
	now = now.Add(6 * time.Minute)
	state := s.Check(context.Background(), true)
	if state.Check.Error != "source_unavailable" || state.Latest == nil || state.Comparison != "available" || state.Check.LastSuccess == nil || !state.Check.LastSuccess.Before(*state.Check.LastAttempt) {
		t.Fatal(state)
	}
}
func TestDisabledDevelopmentUnsupportedAndInvalidRelease(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); json.NewEncoder(w).Encode(fixtureRelease()) }))
	defer server.Close()
	for _, tc := range []struct {
		source      string
		auto        bool
		version, os string
		manualHits  int
	}{{server.URL, false, "0.1.0", "linux", 1}, {server.URL, true, "development", "linux", 1}, {"", true, "0.1.0", "linux", 0}, {server.URL, true, "0.1.0", "darwin", 0}} {
		build := identity()
		build.Version = tc.version
		build.OS = tc.os
		s := New(tc.source, tc.auto, build, server.Client())
		before := hits.Load()
		s.Check(context.Background(), true)
		if hits.Load() != before {
			t.Fatal("unexpected automatic network access")
		}
		state := s.Check(context.Background(), false)
		if hits.Load()-before != int32(tc.manualHits) {
			t.Fatal(tc)
		}
		if tc.version == "development" && state.Comparison != "uncomparable" {
			t.Fatal(state)
		}
	}
	for _, mutate := range []func(*Release){func(r *Release) { r.NotesURL = "javascript:alert(1)" }, func(r *Release) { r.File = "../program" }, func(r *Release) { r.Version = "0.2.0-local" }, func(r *Release) { r.Commit = "unknown" }, func(r *Release) { r.SHA256 = "invalid" }} {
		bad := fixtureRelease()
		mutate(&bad)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(bad) }))
		state := New(server.URL, true, identity(), server.Client()).Check(context.Background(), false)
		server.Close()
		if state.Check.Error != "invalid_manifest" || state.Latest != nil {
			t.Fatal(state)
		}
	}
}
func TestCompareCurrentAndAhead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(fixtureRelease()) }))
	defer server.Close()
	for version, want := range map[string]string{"0.2.0": "current", "0.3.0": "ahead"} {
		build := identity()
		build.Version = version
		s := New(server.URL, true, build, server.Client())
		if got := s.Check(context.Background(), false).Comparison; got != want {
			t.Fatal(got)
		}
	}
}

func TestInvalidAndOversizedResponsesNeverReplaceCache(t *testing.T) {
	for _, body := range []string{`{"version":`, strings.Repeat("x", 65537)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		state := New(server.URL, true, identity(), server.Client()).Check(context.Background(), false)
		server.Close()
		if state.Check.Error != "invalid_manifest" || state.Latest != nil {
			t.Fatal(state)
		}
	}
}
func TestCanceledLifecycleDoesNotCheck(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer server.Close()
	s := New(server.URL, true, identity(), server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
	if hits.Load() != 0 {
		t.Fatal("canceled lifecycle performed a request")
	}
}
