package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/tests/testkit"
)

func TestDeepSeekCacheUsageAndInvocationPricingSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		bad         bool
	}{
		{"hit", `"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20`, false},
		{"invalid-hit", `"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":101`, true},
		{"invalid-miss", `"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":21`, true},
		{"negative", `"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":-1`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "model.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			catalog, err := pricing.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC)
			// Replace the tariff after request dispatch, before receiving usage.
			replacement := `{"schema_version":1,"version":"next","currency":"USD","providers":[{"id":"deepseek","origins":["https://api.deepseek.com"],"schedule":{"holiday_timezone":"UTC"},"models":[{"id":"deepseek-flash","off_peak":{"input_micros_per_million":1000000,"output_micros_per_million":1000000,"cached_input_micros_per_million":1000000},"peak":{"input_micros_per_million":1000000,"output_micros_per_million":1000000,"cached_input_micros_per_million":1000000}}]}]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !testkit.DrainRequestBody(t, r) {
					return
				}
				if err := catalog.Reload(strings.NewReader(replacement)); err != nil {
					t.Error(err)
				}
				fmt.Fprintf(w, `{"id":"result","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{%s}}`, tc.usage)
			}))
			defer server.Close()
			authorizer := controlledAuthorization(func(_ context.Context, p core.Principal, _ policy.CheckRequest) error { return p.Validate() })
			execution := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
			client := NewClient(db, Options{Authorizer: authorizer, Operations: operation.NewService(db, operation.Options{Authorizer: authorizer}), HTTPClient: server.Client(),
				CheckReservation: func(_ context.Context, _ Request, p Provider) error {
					if p.Pricing.InputMicrosPerMillion != 300000 {
						t.Error("reservation received wrong snapshot")
					}
					return nil
				},
				CheckDestination: func(context.Context, core.Execution, core.ID, string) error { return nil },
				Resolve: func(context.Context, core.Principal, core.ID) (Provider, error) {
					q, _ := catalog.Lookup("deepseek", "https://api.deepseek.com", "deepseek-flash", at)
					return Provider{Protocol: DeepSeekChat, Model: Model{ID: "model", Name: "deepseek-flash", ResourceID: "resource", PricingInfo: &PricingInfo{Source: "builtin", Version: q.Version}}, Endpoint: server.URL, Pricing: &Pricing{Currency: "USD", InputMicrosPerMillion: q.Current.Input, OutputMicrosPerMillion: q.Current.Output, CachedInputMicrosPerMillion: q.Current.Cached}}, nil
				},
			})
			stream, err := client.Generate(t.Context(), Request{Execution: execution, ModelID: "model", ReservationID: "reservation", Inputs: []Input{{Role: User, Text: "hi"}}, MaxOutputTokens: 16})
			if tc.bad {
				if core.CodeOf(err) != core.Unavailable {
					t.Fatalf("invalid usage accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			receipt, err := NewReceipts(db).ByOperation(t.Context(), "owner", "operation")
			if err != nil {
				t.Fatal(err)
			}
			// 20 uncached * .3 + 80 cached * .006 + 10 output * 1.2 = 18.48 micros, rounded up.
			if receipt.Usage.CachedTokens != 80 || receipt.Charged.Micros != 19 || receipt.Pricing.InputMicrosPerMillion != 300000 || receipt.PricingInfo.Version != "2026-10-23" {
				t.Fatalf("incorrect billing snapshot: %+v", receipt)
			}
		})
	}
}

func TestTieredPricingUsesActualUsageAndRetainsReceipt(t *testing.T) {
	price := &Pricing{Currency: "CNY", InputMicrosPerMillion: 2000000, OutputMicrosPerMillion: 8000000, CachedInputMicrosPerMillion: 400000,
		Tiers: []PricingTier{{OutputTokensAbove: 199, InputMicrosPerMillion: 3000000, OutputMicrosPerMillion: 14000000, CachedInputMicrosPerMillion: 600000}, {InputTokensAbove: 31999, InputMicrosPerMillion: 4000000, OutputMicrosPerMillion: 16000000, CachedInputMicrosPerMillion: 800000}}}
	for _, tc := range []struct{ input, output, cached, want int64 }{
		{31999, 199, 1000, 63990},
		{31999, 200, 1000, 96397},
		{32000, 199, 1000, 127984},
		{32000, 200, 1000, 128000},
	} {
		got, err := charge(price, Usage{InputTokens: tc.input, OutputTokens: tc.output, CachedTokens: tc.cached})
		if err != nil || got.Currency != "CNY" || got.Micros != tc.want {
			t.Fatalf("%+v: %+v %v", tc, got, err)
		}
	}
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	c := f.Client.(*client)
	resolve := c.options.Resolve
	c.options.Resolve = func(ctx context.Context, principal core.Principal, id core.ID) (Provider, error) {
		p, err := resolve(ctx, principal, id)
		p.Pricing = &Pricing{Currency: "CNY", InputMicrosPerMillion: 1, Tiers: []PricingTier{{InputTokensAbove: 3, InputMicrosPerMillion: 2000000, OutputMicrosPerMillion: 3000000}}}
		p.Model.PricingInfo = &PricingInfo{Source: "builtin", Version: "fixture", Automatic: p.Pricing}
		return p, err
	}
	stream, err := c.Generate(t.Context(), f.Request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	receipt, err := f.AwaitReceipt(t.Context(), f.Request.Execution.OperationID)
	if err != nil || receipt.Charged == nil || *receipt.Charged != (core.Money{Currency: "CNY", Micros: 11}) || receipt.Pricing.InputMicrosPerMillion != 2000000 || len(receipt.Pricing.Tiers) != 0 || receipt.PricingInfo.Version != "fixture" {
		t.Fatalf("receipt lost effective tariff: %+v %v", receipt, err)
	}
}
