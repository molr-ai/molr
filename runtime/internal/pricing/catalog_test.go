package pricing

import (
	"bytes"
	"encoding/json"
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
		if current != old {
			t.Fatal("failed reload changed snapshot")
		}
	}
	updated := bytes.Replace(builtin, []byte(`"version": "2026-09-25"`), []byte(`"version": "next"`), 1)
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
				if !ok || (q.Version == "next" && q.Current.Input != 400000) || (q.Version == "2026-09-25" && q.Current.Input != 300000) {
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
