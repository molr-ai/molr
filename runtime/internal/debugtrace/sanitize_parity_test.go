package debugtrace

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSanitizeKeepsSensitiveKeysNestedValuesAndLimits(t *testing.T) {
	fields := map[string]any{"ordinary": "keep this text", "count": json.Number("9007199254740993")}
	for _, name := range []string{"authorization", "API_KEY", "api-key", "pAsSwOrD", "SeCrEt", "ACCESS_TOKEN", "REFRESH-TOKEN", "cookie", "Set-Cookie", "a_p_i---k_e_y"} {
		fields[name] = "synthetic-sensitive-value"
	}
	payload := map[string]any{"fields": fields, "body": `{"password":"synthetic-sensitive-value","number":9007199254740993}`, "array": []any{"Bearer synthetic-sensitive-value", map[string]any{"api-key": "synthetic-sensitive-value"}}, "text": "safe prefix sk-abcdefghijklmnop safe suffix"}
	raw, status := sanitize(payload, 1<<20)
	if status != "redacted" || strings.Contains(string(raw), "synthetic-sensitive-value") || strings.Contains(string(raw), "sk-abcdefghijklmnop") || !strings.Contains(string(raw), "9007199254740993") || !strings.Contains(string(raw), "keep this text") {
		t.Fatal(status, string(raw))
	}
	var nested any = "bottom"
	for range 68 {
		nested = map[string]any{"child": nested}
	}
	cases := []any{nil, map[string]any{}, payload, nested, "plain text", `{"API_KEY":"fake"} trailing data`, `["Bearer fake-token",{"secret":"fake"}]`, "{invalid JSON Bearer fake-token", json.RawMessage(`{"broken":`), func() {}}
	for i, value := range cases {
		for _, limit := range []int{-1, 0, 8, 64, 1024, 1 << 20} {
			got, gs := sanitize(value, limit)
			want, ws := legacySanitize(value, limit)
			if gs != ws || string(got) != string(want) {
				t.Fatalf("case %d limit %d: got %s %s want %s %s", i, limit, gs, got, ws, want)
			}
		}
	}
	// Redaction may grow a small input beyond the payload limit.
	value := map[string]string{"secret": "x"}
	encoded, _ := json.Marshal(value)
	if raw, status := sanitize(value, len(encoded)); status != "too_large" || len(raw) != 0 {
		t.Fatal("post-redaction limit lost", status, string(raw))
	}
}

func TestSanitizeParallelCapturesKeepIndependentResults(t *testing.T) {
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				marker := fmt.Sprintf("worker-%d-item-%d", worker, i)
				value := map[string]any{"API_KEY": "synthetic-sensitive-value", "marker": marker, "nested": `{"Set-Cookie":"fake"}`}
				raw, status := sanitize(value, 4096)
				if status != "redacted" || !strings.Contains(string(raw), marker) || strings.Contains(string(raw), "synthetic-sensitive-value") || strings.Contains(string(raw), "fake") {
					t.Errorf("capture mixed/leaked: %s %s", status, raw)
				}
			}
		}()
	}
	wg.Wait()
}

func FuzzSanitizeMatchesReference(f *testing.F) {
	f.Add(`{"Api_Key":"fixture","Body":"{\"password\":\"fixture\"}","Text":"Bearer fixture"}`, 1024)
	f.Add(`["plain",9007199254740993,{"Set-Cookie":"fixture"}]`, 64)
	f.Fuzz(func(t *testing.T, input string, limit int) {
		if len(input) > 8192 {
			return
		}
		limit = limit % 16384
		raw := json.RawMessage(input)
		got, gs := sanitize(raw, limit)
		want, ws := legacySanitize(raw, limit)
		if gs != ws || string(got) != string(want) {
			t.Fatalf("different sanitization: %s %s / %s %s", gs, got, ws, want)
		}
	})
}

func TestSanitizePreservesUnchangedJSONStringPrefixes(t *testing.T) {
	value := `{ "z": 2, "a": { "text": "hello" } }`
	for _, payload := range []any{value, map[string]any{"secret": "remove", "Inputs": []any{map[string]any{"Role": "tool", "Text": value}}}} {
		raw, status := sanitize(payload, 4096)
		var result any
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if text, ok := result.(string); ok {
			if text != value || status != "captured" {
				t.Fatal("unchanged JSON string rewritten", string(raw))
			}
		} else {
			row := result.(map[string]any)
			if row["secret"] != "[REDACTED]" || row["Inputs"].([]any)[0].(map[string]any)["Text"] != value || status != "redacted" {
				t.Fatal("sibling redaction changed context", string(raw))
			}
		}
	}
}
