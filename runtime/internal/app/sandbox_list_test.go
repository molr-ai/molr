package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func TestSandboxListBoundsConcurrencyPreservesOrderAndCancels(t *testing.T) {
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	f.ad.networkConfig.AllowedPrivateIPs = []string{"127.0.0.1"}
	router, err := newSandboxRouter(f.ad, nil, make([]byte, 32))
	need(t, err)
	var blocking atomic.Bool
	var sampleDelay atomic.Bool
	var active, peak atomic.Int64
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	defer close(release)
	for i := range 6 {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sampleDelay.Load() {
				time.Sleep(150 * time.Millisecond)
			}
			if blocking.Load() {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				entered <- struct{}{}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			info := sm.Info{Protocol: sm.ProtocolVersion, Enabled: true, Storage: true, Capabilities: platform.Capabilities{OS: "linux", Reason: "available"}}
			_ = json.NewEncoder(w).Encode(map[string]any{"Data": info})
		}))
		t.Cleanup(server.Close)
		sum := sha256.Sum256(server.Certificate().Raw)
		raw, _ := json.Marshal(map[string]any{"Name": fmt.Sprintf("node-%d", i), "Endpoint": server.URL, "Fingerprint": hex.EncodeToString(sum[:]), "Token": strings.Repeat("synthetic-token-", 4), "Mutation": core.Mutation{IdempotencyKey: fmt.Sprintf("connect-%d", i)}})
		_, err := router.admin(t.Context(), f.owner, "connect", raw)
		need(t, err)
	}
	before, err := router.listSandboxes(t.Context(), f.owner)
	need(t, err)
	if len(before) != 7 || !before[0].Local {
		t.Fatal(before)
	}
	after, err := router.listSandboxes(t.Context(), f.owner)
	need(t, err)
	for i := range before {
		if before[i].ID != after[i].ID || after[i].State != "available" {
			t.Fatal("changed order or availability", after)
		}
	}
	// Same six nodes and fixed sampling work, comparing serialized checks with
	// the production bounded list. Timing is reported, not used as a flaky gate.
	sampleDelay.Store(true)
	serialStart := time.Now()
	for _, v := range before[1:] {
		c, err := router.connection(t.Context(), f.owner, v.ID)
		need(t, err)
		_ = router.checkSandbox(t.Context(), f.owner, c)
	}
	serial := time.Since(serialStart)
	parallelStart := time.Now()
	_, err = router.listSandboxes(t.Context(), f.owner)
	need(t, err)
	t.Logf("six nodes, 150ms sampling: serial=%s bounded-parallel=%s", serial, time.Since(parallelStart))
	sampleDelay.Store(false)
	blocking.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := router.listSandboxes(ctx, f.owner); result <- err }()
	for range sandboxListConcurrency {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("remote checks did not overlap")
		}
	}
	select {
	case <-entered:
		t.Fatal("concurrency limit exceeded")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if core.CodeOf(err) != core.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop waiting requests")
	}
	if peak.Load() != sandboxListConcurrency {
		t.Fatal("peak", peak.Load())
	}
}
