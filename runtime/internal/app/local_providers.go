package app

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"net/http"
	"strings"
	"sync"
	"time"
)

type LocalProvider struct {
	Name       string
	Preset     string
	Endpoint   string
	State      string
	Models     []string
	ResourceID core.ID
}

var localProviderOrigins = []string{"http://127.0.0.1:11434", "http://127.0.0.1:8080", "http://127.0.0.1:8000", "http://127.0.0.1:30000"}

func probeLocalProvider(ctx context.Context, client *http.Client, origin string) *LocalProvider {
	result := &LocalProvider{Name: "OpenAI compatible", Preset: "custom", Endpoint: origin + "/v1", Models: []string{}}
	// Both requests only read catalog metadata. Never generate or load models.
	for _, path := range []string{"/api/tags", "/v1/models"} {
		req, err := http.NewRequestWithContext(ctx, "GET", origin+path, nil)
		if err != nil {
			return nil
		}
		req.Header.Set("Accept", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return nil
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		response.Body.Close()
		if readErr != nil || len(data) > 1<<20 {
			return nil
		}
		if response.StatusCode == 401 || response.StatusCode == 403 {
			var failure struct {
				Error json.RawMessage `json:"error"`
			}
			if path == "/v1/models" && json.Unmarshal(data, &failure) == nil && len(failure.Error) > 0 && string(failure.Error) != "null" {
				result.State = "authentication_required"
				return result
			}
			continue
		}
		if response.StatusCode != 200 {
			continue
		}
		var body struct {
			Models []struct{ Name string }
			Data   []struct{ ID string }
		}
		if json.Unmarshal(data, &body) != nil {
			continue
		}
		var names []string
		if path == "/api/tags" && body.Models != nil {
			result.Name = "Ollama compatible"
			result.Preset = "ollama"
			for _, m := range body.Models {
				names = append(names, m.Name)
			}
		} else if path == "/v1/models" && body.Data != nil {
			for _, m := range body.Data {
				names = append(names, m.ID)
			}
		} else {
			continue
		}
		if len(names) > 500 {
			return nil
		}
		seen := map[string]bool{}
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" || len(name) > 256 {
				return nil
			}
			if !seen[name] {
				result.Models = append(result.Models, name)
				seen[name] = true
			}
		}
		result.State = "ready"
		if len(result.Models) == 0 {
			result.State = "empty"
		}
		return result
	}
	return nil
}
func (a adapters) discoverLocalProviders(ctx context.Context, p core.Principal) ([]LocalProvider, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	found := make([]*LocalProvider, len(localProviderOrigins))
	var wait sync.WaitGroup
	for i, o := range localProviderOrigins {
		wait.Add(1)
		go func(i int, o string) {
			defer wait.Done()
			requestCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: a.privateAddresses(), OwnerBinding: func(_ context.Context, actor core.Principal, _ core.ID) (platform.NetworkBinding, error) {
				if actor != p || actor.SessionID == "" {
					return platform.NetworkBinding{}, denied()
				}
				return platform.NetworkBinding{Origin: o}, nil
			}})
			client := guard.OwnerHTTPClient(p, "", o)
			defer client.CloseIdleConnections()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			found[i] = probeLocalProvider(requestCtx, client, o)
		}(i, o)
	}
	wait.Wait()
	owner, err := persistence.CatalogOwner(ctx, a.store, p.OwnerID)
	if err != nil {
		return nil, err
	}
	rows, err := persistence.Scan[resource.Resource](ctx, a.store, "business.resources")
	if err != nil {
		return nil, err
	}
	out := []LocalProvider{}
	for _, item := range found {
		if item == nil {
			continue
		}
		for _, r := range rows {
			if r.OwnerID != owner || r.Kind != resource.Model {
				continue
			}
			b, err := resource.NewRepository(a.store).Binding(ctx, p.OwnerID, r.ID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSuffix(strings.TrimSuffix(b.Endpoint, "/chat/completions"), "/") == item.Endpoint {
				item.ResourceID = r.ID
				break
			}
		}
		out = append(out, *item)
	}
	return out, nil
}
