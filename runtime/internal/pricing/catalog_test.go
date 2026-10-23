package pricing

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeepSeekScheduleAndExactModelMatching(t *testing.T) {
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ at, period string }{
		{"2026-09-24T00:59:59Z", "off_peak"}, {"2026-09-24T01:00:00Z", "peak"},
		{"2026-09-24T03:59:59Z", "peak"}, {"2026-09-24T04:00:00Z", "off_peak"},
		{"2026-09-24T06:00:00Z", "peak"}, {"2026-09-24T10:00:00Z", "off_peak"},
		{"2026-09-25T02:00:00Z", "off_peak"}, // Chinese holiday on a Friday.
		{"2026-09-20T02:00:00Z", "off_peak"}, // Sunday, even though a makeup workday.
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		for _, name := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
			q, ok := c.Lookup("deepseek", "https://api.deepseek.com/v1/chat/completions", name, at)
			want := Rates{150000, 600000, 3000}
			if tc.period == "peak" {
				want = Rates{300000, 1200000, 6000}
			}
			if !ok || q.Period != tc.period || q.Current != want || q.Reserve != (Rates{300000, 1200000, 6000}) || !q.CalendarKnown {
				t.Fatalf("%s %s: %+v", name, tc.at, q)
			}
		}
	}
	at := time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC)
	q, ok := c.Lookup("", "https://api.deepseek.com", "deepseek-v4-pro", at)
	if !ok || q.Current != (Rates{1320000, 3960000, 44000}) {
		t.Fatalf("pro: %+v", q)
	}
	for _, tc := range []struct{ hint, endpoint, model string }{
		{"custom", "https://api.deepseek.com", "deepseek-flash"}, {"openai", "https://api.deepseek.com", "deepseek-flash"},
		{"deepseek", "https://proxy.example.com", "deepseek-flash"}, {"deepseek", "https://api.deepseek.com.evil.example", "deepseek-flash"},
		{"deepseek", "http://api.deepseek.com", "deepseek-flash"}, {"deepseek", "https://api.deepseek.com", "deepseek-flash-unknown"},
		{"deepseek", "https://api.deepseek.com", "deepseek-chat"},
	} {
		if _, ok := c.Lookup(tc.hint, tc.endpoint, tc.model, at); ok {
			t.Fatalf("incorrect match: %+v", tc)
		}
	}
	q, _ = c.Lookup("", "https://api.deepseek.com", "deepseek-flash", time.Date(2027, 1, 4, 2, 0, 0, 0, time.UTC))
	if q.CalendarKnown || q.Period != "peak" {
		t.Fatalf("uncovered calendar should use conservative weekday schedule: %+v", q)
	}
}

func TestReloadValidatesAndPreservesSnapshots(t *testing.T) {
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC)
	old, _ := c.Lookup("", "https://api.deepseek.com", "deepseek-flash", at)
	for _, invalid := range []string{
		`{}`, string(builtin) + `{}`, strings.Replace(string(builtin), `"currency": "USD"`, `"currency": "EUR"`, 1),
		strings.Replace(string(builtin), `"input_micros_per_million": 150000,`, "", 1),
		strings.Replace(string(builtin), `"input_micros_per_million": 150000`, `"input_micros_per_million": -1`, 1),
		strings.Replace(string(builtin), `"cached_input_micros_per_million": 3000`, `"cached_input_micros_per_million": 999999`, 1),
		strings.Replace(string(builtin), `"peak": {`, `"peak": null, "unknown": {`, 1),
		strings.Replace(string(builtin), `"end": "04:00"`, `"end": "09:00"`, 1),
	} {
		if c.Reload(strings.NewReader(invalid)) == nil {
			t.Fatal("accepted invalid rules")
		}
		current, _ := c.Lookup("", "https://api.deepseek.com", "deepseek-flash", at)
		if !reflect.DeepEqual(current, old) {
			t.Fatal("failed reload changed snapshot")
		}
	}
	updated := bytes.Replace(builtin, []byte(`"version": "2026-10-23"`), []byte(`"version": "next"`), 1)
	updated = bytes.Replace(updated, []byte(`"input_micros_per_million": 300000`), []byte(`"input_micros_per_million": 400000`), 1)
	if err := c.Reload(bytes.NewReader(updated)); err != nil {
		t.Fatal(err)
	}
	next, _ := c.Lookup("", "https://api.deepseek.com", "deepseek-flash", at)
	if next.Version != "next" || next.Current.Input != 400000 || old.Current.Input != 300000 {
		t.Fatalf("snapshot isolation failed: %+v %+v", old, next)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 40; j++ {
				q, ok := c.Lookup("", "https://api.deepseek.com", "deepseek-flash", at)
				if !ok || (q.Version == "next" && q.Current.Input != 400000) || (q.Version == "2026-10-23" && q.Current.Input != 300000) {
					t.Error("torn snapshot")
				}
			}
		})
	}
	for i := 0; i < 20; i++ {
		data := builtin
		if i%2 == 0 {
			data = updated
		}
		if err := c.Reload(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestRatesRequireExplicitIntegers(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"input_micros_per_million":0,"output_micros_per_million":0}`, `{"input_micros_per_million":0.1,"output_micros_per_million":0,"cached_input_micros_per_million":0}`} {
		var rates Rates
		if json.Unmarshal([]byte(raw), &rates) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var rates Rates
	if err := json.Unmarshal([]byte(`{"input_micros_per_million":0,"output_micros_per_million":0,"cached_input_micros_per_million":0}`), &rates); err != nil {
		t.Fatal(err)
	}
}

func TestProviderPricesMatchExactProductAndCurrency(t *testing.T) {
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		provider, endpoint, model, currency string
		rates                               Rates
	}{
		{"zhipu", "https://open.bigmodel.cn/api/paas/v4/chat/completions", "glm-5.3", "CNY", Rates{8000000, 28000000, 2000000}},
		{"zai", "https://api.z.ai/api/paas/v4/chat/completions", "glm-5.3", "USD", Rates{1400000, 4400000, 260000}},
		{"openai", "https://api.openai.com:443/v1/chat/completions", "gpt-4.1", "USD", Rates{2000000, 8000000, 500000}},
		{"minimax", "https://api.minimax.cn/v1/chat/completions", "MiniMax-M3.1-Flash", "CNY", Rates{1000000, 4000000, 100000}},
		{"minimax", "https://api.minimax.io/v1/chat/completions", "MiniMax-M3.1-Flash", "USD", Rates{150000, 600000, 15000}},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions", "gemini-3.1-pro-preview-customtools", "USD", Rates{2000000, 12000000, 200000}},
		{"anthropic", "https://api.anthropic.com/v1/messages", "claude-sonnet-5-5", "USD", Rates{2000000, 10000000, 100000}},
		{"anthropic", "https://api.anthropic.com/v1/messages", "claude-haiku-4-5-20251001", "USD", Rates{1000000, 5000000, 100000}},
		{"moonshot", "https://api.moonshot.cn/v1/chat/completions", "kimi-k2.6", "CNY", Rates{6500000, 27000000, 1100000}},
		{"moonshot", "https://api.moonshot.ai/v1/chat/completions", "kimi-k2.6", "USD", Rates{950000, 4000000, 160000}},
		{"zhipu", "https://open.bigmodel.cn/api/paas/v4", "glm-4.7-flash", "CNY", Rates{}},
	} {
		for _, hint := range []string{tc.provider, ""} {
			q, ok := c.Lookup(hint, tc.endpoint, tc.model, at)
			if !ok || q.Currency != tc.currency || q.Current != tc.rates || q.Period != "" || q.SourceURL == "" {
				t.Fatalf("%+v: %+v", tc, q)
			}
		}
		for _, hint := range []string{"custom", "unrelated"} {
			if _, ok := c.Lookup(hint, tc.endpoint, tc.model, at); ok {
				t.Fatalf("incorrect preset match: %+v", tc)
			}
		}
		for _, endpoint := range []string{strings.Replace(tc.endpoint, "https://", "http://", 1), "https://proxy.example/v1", tc.endpoint + "?product=coding", tc.endpoint + "#fragment"} {
			if _, ok := c.Lookup(tc.provider, endpoint, tc.model, at); ok {
				t.Fatal("incorrect origin", endpoint)
			}
		}
		if _, ok := c.Lookup(tc.provider, tc.endpoint, tc.model+"-unknown", at); ok {
			t.Fatal("guessed model price")
		}
	}
	for _, tc := range []struct{ provider, endpoint, model string }{
		{"zai", "https://api.z.ai/api/coding/paas/v4/chat/completions", "glm-5.3"},
		{"zhipu", "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions", "glm-5.3"},
		{"openai", "https://api.openai.com/v1", "gpt-6-astra"}, // Separate cache writes are not yet represented.
		{"moonshot", "https://api.moonshot.cn/v1", "kimi-k3"},
		{"moonshot", "https://api.kimi.com/coding/v1", "kimi-k2.6"},
		{"openai", "https://api.openai.com/v1/responses", "gpt-5.4"}, // ChatGPT subscription endpoint.
	} {
		if _, ok := c.Lookup(tc.provider, tc.endpoint, tc.model, at); ok {
			t.Fatal("unsupported tariff", tc)
		}
	}
}

func TestTierSnapshotReserveAndPromotionExpiry(t *testing.T) {
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	q, ok := c.Lookup("zhipu", "https://open.bigmodel.cn/api/paas/v4", "glm-4.7", at)
	if !ok || len(q.Tiers) != 2 || q.Tiers[0].OutputTokensAbove != 199 || q.Tiers[1].InputTokensAbove != 31999 || q.Reserve != (Rates{4000000, 16000000, 800000}) {
		t.Fatal(q)
	}
	q.Tiers[0].Rates.Input = 999
	next, _ := c.Lookup("zhipu", "https://open.bigmodel.cn/api/paas/v4", "glm-4.7", at)
	if next.Tiers[0].Rates.Input != 3000000 {
		t.Fatal("mutable catalog escaped")
	}
	for _, tc := range []struct {
		at        string
		available bool
	}{{"2026-11-30T23:59:59Z", true}, {"2026-12-01T00:00:00Z", false}} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		_, ok := c.Lookup("gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-4-argon", at)
		if ok != tc.available {
			t.Fatal("expired promotional price", tc)
		}
	}
	for _, replacement := range []struct{ from, to string }{
		{`"currency": "CNY"`, `"currency": "EUR"`},
		{`"input_tokens_above": 31999`, `"input_tokens_above": -1`},
		{`"output_tokens_above": 199`, `"output_tokens_above": -1`},
		{`"2026-12-01T00:00:00Z"`, `"invalid"`},
		{`"/api/paas/v4"`, `"/api/../coding"`},
	} {
		data := strings.Replace(string(builtin), replacement.from, replacement.to, 1)
		if c.Reload(strings.NewReader(data)) == nil {
			t.Fatal("accepted invalid rules", replacement)
		}
	}
	var doc document
	if err := json.Unmarshal(builtin, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Providers[1].Models[5].Tiers[0].Rates = nil
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if c.Reload(bytes.NewReader(data)) == nil {
		t.Fatal("missing tier price treated as free")
	}
}
