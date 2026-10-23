//go:build linux || darwin

package installation

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOfficialMirrorScope(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/molr-ai/molr/releases/download/v0.2.0/molr_0.2.0_linux_amd64.tar.gz",
		"https://molr.ai/releases/stable/manifest.json",
		"https://molr.ai/releases/versions/0.2.0.json",
	} {
		if !strings.HasPrefix(officialMirror(raw), "https://cdn.molr.ai/releases/") {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{
		"https://github.com/other/molr/releases/download/v0.2.0/molr_0.2.0_linux_amd64.tar.gz",
		"https://github.com/molr-ai/molr/releases/download/v0.2.0/molr_0.3.0_linux_amd64.tar.gz",
		"https://github.com/molr-ai/molr/releases/download/v0.2.0-rc.1/molr_0.2.0-rc.1_linux_amd64.tar.gz",
		"https://molr.ai/releases/preview/manifest.json",
		"https://molr.ai/releases/nightly/manifest.json",
		"https://molr.ai.evil.example/releases/stable/manifest.json",
		"https://molr.ai/releases/stable/manifest.json?token=secret",
	} {
		if officialMirror(raw) != "" {
			t.Fatal(raw)
		}
	}
}

func TestPackageMirrorRestartsPartialDownloadAndChecksDigest(t *testing.T) {
	e, _ := fixture(t)
	payload := "verified package"
	primaryOK, corruptMirror := false, false
	var hosts []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hosts = append(hosts, r.Host)
		if r.Host == "github.com" && !primaryOK {
			w.Header().Set("Content-Length", "99999")
			io.WriteString(w, "discard partial bytes")
		} else if corruptMirror {
			io.WriteString(w, "corrupt")
		} else {
			io.WriteString(w, payload)
		}
	}))
	defer server.Close()
	original := http.DefaultTransport
	transport := server.Client().Transport.(*http.Transport).Clone()
	// Keep the requested host/path while routing both HTTPS origins to our fixture.
	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := tls.Dialer{Config: transport.TLSClientConfig}
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = original }()
	raw := "https://github.com/molr-ai/molr/releases/download/v0.2.0/molr_0.2.0_linux_amd64.tar.gz"
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	for _, scenario := range []string{"fallback", "primary", "corrupt", "custom", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			hosts = nil
			primaryOK, corruptMirror = scenario == "primary", scenario == "corrupt"
			url := raw
			if scenario == "custom" {
				url = strings.Replace(raw, "molr-ai/molr", "custom/molr", 1)
			}
			archive, err := os.Create(filepath.Join(t.TempDir(), "archive"))
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			if scenario == "cancelled" {
				ctx, cancel := context.WithCancel(e.Ctx)
				cancel()
				e.Ctx = ctx
			}
			err = e.fetchPackage(url, archive, expected)
			if scenario == "fallback" || scenario == "primary" {
				if err != nil {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(archive.Name())
				if string(data) != payload {
					t.Fatal(string(data))
				}
			} else if err == nil {
				t.Fatal("accepted failed/corrupt download")
			}
			switch scenario {
			case "fallback", "corrupt":
				if strings.Join(hosts, ",") != "github.com,cdn.molr.ai" {
					t.Fatal(hosts)
				}
			case "primary", "custom":
				if strings.Join(hosts, ",") != "github.com" {
					t.Fatal(hosts)
				}
			case "cancelled":
				if len(hosts) != 0 {
					t.Fatal(hosts)
				}
			}
		})
	}
}

func TestDownloadLowSpeedWatchdog(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var received atomic.Int64
		done := make(chan struct{})
		go func() { watchDownload(ctx, cancel, &received, 40*time.Millisecond, 10); close(done) }()
		if !stalled {
			for i := 0; i < 12; i++ {
				received.Add(20)
				time.Sleep(10 * time.Millisecond)
			}
			if ctx.Err() != nil {
				t.Fatal("healthy download cancelled")
			}
			cancel()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("watchdog did not stop blocked download")
		}
	}
}

func TestDownloadAbortsStalledTLSAndBody(t *testing.T) {
	e, _ := fixture(t)
	t.Run("TLS handshake", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		done := make(chan struct{})
		defer close(done)
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			<-done
		}()
		start := time.Now()
		err = e.fetch("https://"+listener.Addr().String()+"/package", io.Discard, 1024, false)
		if err == nil || !strings.Contains(err.Error(), "TLS handshake timeout") || time.Since(start) > 12*time.Second {
			t.Fatal(err, time.Since(start))
		}
	})
	t.Run("body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1024")
			io.WriteString(w, "x")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		defer server.Close()
		start := time.Now()
		err := e.fetch(server.URL, io.Discard, 1024, false)
		if err == nil || !strings.Contains(err.Error(), "below 32 KiB/s") || time.Since(start) > 20*time.Second {
			t.Fatal(err, time.Since(start))
		}
	})
}
