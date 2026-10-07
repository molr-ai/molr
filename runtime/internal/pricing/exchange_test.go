package pricing

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestExchangePreservesSnapshotsAndIntegerAmounts(t *testing.T) {
	e, err := NewExchange(nil)
	if err != nil {
		t.Fatal(err)
	}
	var reader ExchangeReader = e
	old := reader.Snapshot()
	usd := core.Money{Currency: "USD", Micros: 1000000}
	converted, err := old.Convert(usd, "CNY", true)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Currency != "CNY" || converted.Micros != 6712590 {
		t.Fatalf("wrong reference conversion: %+v", converted)
	}
	if usd.Micros != 1000000 || usd.Currency != "USD" {
		t.Fatal("source amount changed")
	}
	one, err := old.Convert(core.Money{Currency: "CNY", Micros: 1}, "USD", true)
	if err != nil || one.Micros != 1 {
		t.Fatal("fractional cost rounded down")
	}
	for _, raw := range []string{`{}`, string(builtinExchange) + `{}`, strings.Replace(string(builtinExchange), `"CNY": 76302`, `"CNY": 0`, 1)} {
		if e.Reload(strings.NewReader(raw)) == nil {
			t.Fatal("invalid rates accepted")
		}
		if e.Snapshot().Version != old.Version {
			t.Fatal("failed reload changed rates")
		}
	}
	next := bytes.Replace(builtinExchange, []byte(`"ecb-2026-09-24"`), []byte(`"next"`), 1)
	next = bytes.Replace(next, []byte(`"CNY": 76302`), []byte(`"CNY": 79569`), 1)
	if err := e.Reload(bytes.NewReader(next)); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().Units["CNY"] != 79569 || old.Units["CNY"] != 76302 {
		t.Fatal("snapshot changed")
	}
	returned := e.Snapshot()
	returned.Units["USD"] = 0
	if e.Snapshot().Units["USD"] != 11367 {
		t.Fatal("snapshot map mutates catalog")
	}
	if _, err := old.Convert(usd, "EUR", true); err == nil {
		t.Fatal("unsupported currency accepted")
	}
	if _, err := old.Convert(core.Money{Currency: "USD", Micros: 1 << 62}, "CNY", true); err == nil {
		t.Fatal("overflow accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for j := 0; j < 50; j++ {
				s := e.Snapshot()
				if s.Version == "next" && s.Units["CNY"] != 79569 {
					t.Error("torn rate snapshot")
				}
			}
		})
	}
	for i := 0; i < 20; i++ {
		if err := e.Reload(bytes.NewReader(builtinExchange)); err != nil {
			t.Fatal(err)
		}
		if err := e.Reload(bytes.NewReader(next)); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
