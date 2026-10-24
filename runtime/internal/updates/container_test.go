package updates

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func containerFixture(version string) map[string]any {
	body := []byte(`{"schemaVersion":2,"fixture":"platform"}`)
	descriptors := []map[string]any{}
	for _, arch := range []string{"amd64", "arm64"} {
		descriptors = append(descriptors, map[string]any{"mediaType": imageManifestType, "digest": fmt.Sprintf("sha256:%x", sha256.Sum256(body)), "size": len(body), "platform": map[string]string{"os": "linux", "architecture": arch}})
	}
	return map[string]any{"schemaVersion": 2, "mediaType": imageIndexType, "manifests": descriptors, "annotations": map[string]string{
		"org.opencontainers.image.version": version, "org.opencontainers.image.revision": strings.Repeat("a", 40), "org.opencontainers.image.created": "2026-10-24T00:00:00Z", "org.opencontainers.image.source": "https://github.com/example/molr",
	}}
}

func TestContainerChecksStableForEveryCurrentChannel(t *testing.T) {
	for _, tc := range []struct{ current, target, comparison string }{
		{"0.1.0", "0.1.0", "current"},
		{"0.1.0", "0.2.0", "available"},
		{"0.1.0-beta.1", "0.1.0", "available"},
		{"0.1.0-rc.1", "0.1.0", "available"},
		{"0.1.0-0.nightly.20261024.145333.g8ef1ffd4ea76", "0.1.0", "available"},
		{"0.1.1-0.nightly.20261025.0.gaaaaaaa", "0.1.0", "ahead"},
	} {
		t.Run(tc.current+"/"+tc.target, func(t *testing.T) {
			indexes, platforms := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					if r.URL.Query().Get("scope") != "repository:example/molr:pull" || r.Header.Get("Authorization") != "" {
						t.Error("invalid anonymous scope")
					}
					w.Write([]byte(`{"token":"artificial-registry-token"}`))
					return
				}
				if r.Header.Get("Authorization") != "Bearer artificial-registry-token" {
					t.Error("missing scoped registry token")
				}
				switch {
				case r.URL.Path == "/v2/example/molr/manifests/latest":
					indexes++
					json.NewEncoder(w).Encode(containerFixture(tc.target))
				case strings.Contains(r.URL.Path, "/manifests/sha256:"):
					platforms++
					w.Write([]byte(`{"schemaVersion":2,"fixture":"platform"}`))
				default:
					t.Errorf("unexpected non-stable lookup: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			current := identity()
			current.Version = tc.current
			current.Distribution = "container"
			current.ContainerImage = "ghcr.io/example/molr"
			service := New("https://example.com/native", true, current, server.Client())
			if service.source != "https://ghcr.io" || !service.Snapshot().SourceConfigured {
				t.Fatal(service.Snapshot())
			}
			service.source = server.URL
			state := service.Check(context.Background(), false)
			if state.Comparison != tc.comparison || state.Latest == nil || state.Latest.Image != "ghcr.io/example/molr:latest" || state.Latest.Version != tc.target || !strings.HasPrefix(state.Latest.Digest, "sha256:") || state.Latest.File != "" || indexes != 1 || platforms != 1 {
				t.Fatal(state, indexes, platforms)
			}
		})
	}
}

func TestContainerMissingStableDoesNotFallBackOrHideRegistryErrors(t *testing.T) {
	for _, missing := range []string{"latest", "token", "platform"} {
		t.Run(missing, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/token":
					if missing == "token" {
						w.WriteHeader(404)
						return
					}
					w.Write([]byte(`{"token":"artificial-registry-token"}`))
				case r.URL.Path == "/v2/example/molr/manifests/latest":
					if missing == "latest" {
						w.WriteHeader(404)
						return
					}
					json.NewEncoder(w).Encode(containerFixture("0.1.0"))
				case strings.Contains(r.URL.Path, "/manifests/sha256:"):
					w.WriteHeader(404)
				default:
					t.Errorf("unexpected fallback: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			current := identity()
			current.Version = "0.1.0-0.nightly.20261024.0.gaaaaaaa"
			current.Distribution = "container"
			current.ContainerImage = "ghcr.io/example/molr"
			service := New("", true, current, server.Client())
			service.source = server.URL
			state := service.Check(context.Background(), false)
			if state.Latest != nil {
				t.Fatal(state)
			}
			if missing == "latest" {
				if state.Comparison != "unavailable" || state.Check.Error != "" || state.Check.LastSuccess == nil {
					t.Fatal(state)
				}
			} else if state.Comparison != "unknown" || state.Check.Error == "" || state.Check.LastSuccess != nil {
				t.Fatal(state)
			}
		})
	}
}

func TestContainerRejectsInvalidOrUnavailableImagesAndRetainsLastSuccess(t *testing.T) {
	for _, kind := range []string{"partial", "channel", "source", "digest", "unauthorized", "bad-platform"} {
		t.Run(kind, func(t *testing.T) {
			fail := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					w.Write([]byte(`{"token":"fixture"}`))
					return
				}
				if strings.Contains(r.URL.Path, "/manifests/sha256:") {
					if fail && kind == "bad-platform" {
						w.Write([]byte("substituted"))
						return
					}
					w.Write([]byte(`{"schemaVersion":2,"fixture":"platform"}`))
					return
				}
				value := containerFixture("0.2.0")
				if fail {
					switch kind {
					case "partial":
						value["manifests"] = []any{}
					case "channel":
						value = containerFixture("0.2.0-rc.1")
					case "source":
						value["annotations"].(map[string]string)["org.opencontainers.image.source"] = "https://github.com/unrelated/repo"
					case "digest":
						w.Header().Set("Docker-Content-Digest", "sha256:"+strings.Repeat("f", 64))
					case "unauthorized":
						w.WriteHeader(401)
						return
					}
				}
				json.NewEncoder(w).Encode(value)
			}))
			defer server.Close()
			current := identity()
			current.Distribution = "container"
			current.ContainerImage = "ghcr.io/example/molr"
			service := New("", true, current, server.Client())
			service.source = server.URL
			now := time.Now()
			service.now = func() time.Time { return now }
			good := service.Check(context.Background(), false)
			if good.Latest == nil {
				t.Fatal(good)
			}
			fail = true
			now = now.Add(2 * time.Minute)
			bad := service.Check(context.Background(), false)
			if bad.Check.Error == "" || bad.Latest == nil || bad.Latest.Digest != good.Latest.Digest {
				t.Fatal(bad)
			}
		})
	}
	current := identity()
	current.Distribution = "container"
	service := New("https://example.com/native", true, current, http.DefaultClient)
	if service.Snapshot().SourceConfigured {
		t.Fatal("local Docker build inherited native update source")
	}
}
