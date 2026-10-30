package app

import (
	"context"
	"math/big"
	"molr.ai/runtime/internal/textfiles"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
)

type adapters struct {
	reserveModelCost bool
	store            persistence.Store
	policy           policy.Authorizer
	dataDir          string
	broker           credential.Broker
	networkConfig    config.Network
	storageConfig    config.Sandbox
	ownPort          uint16
	pricing          *pricing.Catalog
	now              func() time.Time
}

func origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Fragment != "" || u.RawQuery != "" {
		return "", core.Invalid("endpoint", "HTTP(S) endpoint required")
	}
	return u.Scheme + "://" + u.Host, nil
}
func (a adapters) providers() map[string]resource.Provider {
	return map[string]resource.Provider{
		"openai-compatible": {Kind: resource.Model, Validate: func(_ context.Context, b resource.Binding) ([]string, error) {
			if b.SandboxID != "" && b.SandboxID != "local" || b.SandboxResource != "" {
				return nil, core.Invalid("sandbox", "use sandbox resource configuration")
			}
			if b.SandboxTarget != "" {
				return nil, core.Invalid("binding", "sandbox target not supported by model provider")
			}
			if b.ChatGPT != nil && (b.PricingProvider != "openai" || b.Endpoint != "https://api.openai.com/v1/responses" || b.ChatGPT.ClientID == "" || b.ChatGPT.ClientID == "dynamic_agent_client") {
				return nil, core.Invalid("chatgpt", "invalid ChatGPT binding")
			}
			switch b.PricingProvider {
			case "", "openai", "openrouter", "deepseek", "bailian", "zhipu", "moonshot", "ollama", "llamacpp", "vllm", "sglang", "anthropic", "gemini", "minimax", "volcengine", "siliconflow", "lmstudio", "zai", "custom":
			default:
				return nil, core.Invalid("pricing_provider", "unknown provider preset")
			}
			o, err := origin(b.Endpoint)
			if err != nil {
				return nil, err
			}
			u, _ := url.Parse(b.Endpoint)
			if address, err := netip.ParseAddr(u.Hostname()); err == nil && u.Scheme == "http" {
				address = address.Unmap()
				approved := false
				for _, raw := range a.networkConfig.AllowedPrivateIPs {
					if netip.MustParseAddr(raw).Unmap() == address.Unmap() {
						approved = true
					}
				}
				if !approved {
					reason := core.NetworkHTTPNotAllowed
					if address.IsPrivate() || address.IsLoopback() || netip.MustParsePrefix("100.64.0.0/10").Contains(address.Unmap()) {
						reason = core.NetworkPrivateAddressDenied
					}
					return nil, core.WithReason(core.Invalid("endpoint", "HTTP endpoint not allowed by server configuration"), reason)
				}
			}
			if u.Scheme != "https" && len(a.networkConfig.AllowedPrivateIPs) == 0 {
				return nil, core.WithReason(core.Invalid("endpoint", "public providers require HTTPS"), core.NetworkHTTPNotAllowed)
			}
			if len(b.AllowedOrigins) != 1 || b.AllowedOrigins[0] != o || b.RootPath != "" {
				return nil, core.Invalid("binding", "exact origin required; directory path is not supported")
			}
			configs := b.Models
			limit := 501
			if b.PricingProvider == "openrouter" {
				limit = resource.MaxProviderModels
			}
			if len(configs) > limit {
				return nil, core.Invalid("models", "too many models")
			}
			names := make(map[string]bool, len(configs))
			for _, m := range configs {
				if names[m.Name] {
					return nil, core.Invalid("models", "duplicate model name")
				}
				names[m.Name] = true
				if m.OutputLimit < 0 || m.OutputLimit > model.MaxOutputLimit || m.ProviderOutputLimit < 0 || m.ProviderOutputLimit > model.MaxOutputLimit || strings.TrimSpace(m.Name) == "" || len(m.Name) > 256 || (m.ContextLimit != 0 && (m.ContextLimit < 256 || m.ContextLimit > 2_000_000)) || (m.ProviderContextLimit != 0 && (m.ProviderContextLimit < 256 || m.ProviderContextLimit > 2_000_000)) {
					return nil, core.Invalid("model", "invalid model limits or pricing")
				}
				for _, p := range []*resource.ModelPricing{m.Pricing, m.ProviderPricing} {
					if p != nil && ((p.AudioInputMicrosPerMillion != nil && *p.AudioInputMicrosPerMillion < 0) || (p.Currency != "USD" && p.Currency != "CNY") || p.InputMicrosPerMillion < 0 || p.OutputMicrosPerMillion < 0 || p.CachedInputMicrosPerMillion < 0 || p.CachedInputMicrosPerMillion > p.InputMicrosPerMillion) {
						return nil, core.Invalid("pricing", "invalid pricing")
					}
				}
			}
			if b.CredentialID != "" && b.CredentialID.Validate() != nil {
				return nil, core.Invalid("credential_id", "invalid")
			}
			return []string{"read", "execute"}, nil
		}, Test: a.testModelConnection, Discover: a.discoverModels, Check: func(_ context.Context, _ resource.Binding) (resource.Status, error) {
			return resource.Status{State: resource.Available, ReasonCode: "configured", CheckedAt: time.Now()}, nil
		}},
		"local-directory": {Kind: resource.Storage, Validate: func(_ context.Context, b resource.Binding) ([]string, error) {
			if b.SandboxID != "" && b.SandboxID != "local" || b.SandboxResource != "" {
				return nil, core.Invalid("sandbox", "use sandbox resource configuration")
			}
			if b.SandboxTarget != "" {
				return nil, core.Invalid("binding", "sandbox target not supported by directory")
			}
			if !filepath.IsAbs(b.RootPath) || filepath.Clean(b.RootPath) == string(filepath.Separator) || b.Endpoint != "" || len(b.Models) != 0 || b.CredentialID != "" {
				return nil, core.Invalid("root_path", "dedicated absolute directory required")
			}
			root, err := filepath.EvalSymlinks(b.RootPath)
			if err != nil {
				return nil, core.Invalid("root_path", "existing directory required")
			}
			if root != filepath.Clean(textfiles.SystemPath(b.RootPath)) {
				return nil, core.Invalid("root_path", "resource path must not contain symbolic links")
			}
			for _, protected := range a.storageConfig.ProtectedRoots {
				if textfiles.Overlaps(root, protected) {
					return nil, core.NewError(core.Forbidden, "installation assets cannot be resources")
				}
			}
			private, err := filepath.EvalSymlinks(a.dataDir)
			if err != nil {
				return nil, err
			}
			if textfiles.Overlaps(root, private) {
				return nil, core.NewError(core.Forbidden, "runtime data cannot be a resource")
			}
			if work := a.storageConfig.WorkRoot; work != "" && textfiles.Overlaps(root, work) {
				return nil, core.NewError(core.Forbidden, "managed assistant workspaces cannot be external resources")
			}
			info, err := os.Stat(root)
			if err != nil || !info.IsDir() {
				return nil, core.Invalid("root_path", "directory required")
			}
			readOnly, err := directoryPolicy(a.storageConfig, root)
			if err != nil {
				return nil, err
			}
			if readOnly {
				return []string{"read"}, nil
			}
			return []string{"read", "write"}, nil
		}, Check: func(_ context.Context, b resource.Binding) (resource.Status, error) {
			info, err := os.Stat(b.RootPath)
			if err != nil || !info.IsDir() {
				return resource.Status{State: resource.Offline, ReasonCode: "directory_unavailable", CheckedAt: time.Now()}, nil
			}
			return resource.Status{State: resource.Available, ReasonCode: "configured", CheckedAt: time.Now()}, nil
		}},
	}
}
func (a adapters) binding(ctx context.Context, p core.Principal, id core.ID) (resource.Resource, resource.Binding, error) {
	repo := resource.NewRepository(a.store)
	r, err := repo.Get(ctx, p.OwnerID, id)
	if err != nil {
		return r, resource.Binding{}, err
	}
	if r.State != resource.Available {
		return r, resource.Binding{}, denied()
	}
	b, err := repo.Binding(ctx, p.OwnerID, id)
	return r, b, err
}
func (a adapters) privateAddresses() []netip.Addr {
	addresses := make([]netip.Addr, 0, len(a.networkConfig.AllowedPrivateIPs))
	for _, raw := range a.networkConfig.AllowedPrivateIPs {
		addresses = append(addresses, netip.MustParseAddr(raw))
	}
	return addresses
}
func (a adapters) network() *platform.Guard {
	return platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: a.privateAddresses(), OwnerBinding: a.ownerNetworkBinding, Binding: func(ctx context.Context, e core.Execution, id core.ID) (platform.NetworkBinding, error) {
		if err := (access{a.store}).execution(ctx, e); err != nil {
			return platform.NetworkBinding{}, err
		}
		_, b, err := a.binding(ctx, e.Principal, id)
		if err != nil {
			return platform.NetworkBinding{}, err
		}
		o, err := origin(b.Endpoint)
		if err != nil {
			return platform.NetworkBinding{}, err
		}
		if err = a.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: policy.Execute, Destination: b.Endpoint}); err != nil {
			return platform.NetworkBinding{}, err
		}
		return platform.NetworkBinding{Origin: o}, nil
	}})
}
func (a adapters) client(ctx context.Context, r model.Request, p model.Provider) (*http.Client, error) {
	o, err := origin(p.Endpoint)
	if err != nil {
		return nil, err
	}
	client := a.network().HTTPClient(r.Execution, p.Model.ResourceID, o)
	client.Timeout = model.DefaultRequestTimeout
	// Reasoning models may wait before sending response headers as well as tokens.
	transport := client.Transport.(*http.Transport)
	transport.ResponseHeaderTimeout = model.DefaultRequestTimeout
	return client, nil
}
func (a adapters) destination(ctx context.Context, e core.Execution, id core.ID, dest string) error {
	_, b, err := a.binding(ctx, e.Principal, id)
	if err != nil {
		return err
	}
	if dest != b.Endpoint {
		return denied()
	}
	return a.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: policy.Execute, Destination: dest})
}
func (a adapters) credentialAccess(ctx context.Context, q credential.Access) error {
	p, err := q.EffectivePrincipal()
	if err != nil {
		return err
	}
	r, b, err := a.binding(ctx, p, q.ResourceID)
	if err != nil {
		return err
	}
	o, err := origin(b.Endpoint)
	if err != nil {
		return err
	}
	if o != q.Origin || b.CredentialID != q.CredentialID {
		return denied()
	}
	value, err := credential.NewRepository(a.store).Get(ctx, p.OwnerID, q.CredentialID)
	if err != nil {
		return err
	}
	expected := r.Provider
	if b.ChatGPT != nil {
		expected = "chatgpt"
	}
	if value.Metadata.Provider != expected {
		return denied()
	}
	return a.policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: r.ID}, ResourceID: r.ID, Action: policy.Execute, Destination: b.Endpoint})
}
func (a adapters) estimate(ctx context.Context, id core.ID, inputs []model.Input, max int) (core.Money, error) {
	provider, err := a.modelCatalog().ForEstimate(ctx, id)
	if core.CodeOf(err) == core.NotFound {
		return core.Money{}, core.WithReason(core.NewError(core.FailedPrecondition, "model unavailable"), core.ModelUnavailable)
	}
	if err != nil {
		return core.Money{}, err
	}
	return estimateProvider(provider, max, a.reserveModelCost)
}
func estimateProvider(provider model.Provider, max int, reserveModelCost bool) (core.Money, error) {
	price := provider.ReservePricing
	if price == nil {
		price = provider.Pricing
	}
	if price == nil {
		return core.Money{}, core.WithReason(core.NewError(core.FailedPrecondition, "model pricing unavailable"), core.ModelPricingMissing)
	}
	if !reserveModelCost {
		return core.Money{Currency: price.Currency}, nil
	}
	// Reserve the full context at peak rates; settlement uses actual usage and
	// the price snapshot resolved for this invocation.
	inputRate := price.InputMicrosPerMillion
	if provider.Model.SupportsAudio && price.AudioInputMicrosPerMillion != nil {
		if *price.AudioInputMicrosPerMillion > inputRate {
			inputRate = *price.AudioInputMicrosPerMillion
		}
	}
	total := new(big.Int).Mul(big.NewInt(int64(provider.Model.ContextWindow())), big.NewInt(inputRate))
	total.Add(total, new(big.Int).Mul(big.NewInt(int64(max)), big.NewInt(price.OutputMicrosPerMillion)))
	total.Add(total, big.NewInt(999999))
	total.Div(total, big.NewInt(1000000))
	if !total.IsInt64() {
		return core.Money{}, core.NewError(core.BudgetExceeded, "estimate overflow")
	}
	return core.Money{Currency: price.Currency, Micros: total.Int64()}, nil
}
