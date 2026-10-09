package model

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelOutputIsNotPersistedAsRecoveryData(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		success bool
	}{
		{"unknown extensions", 200, `{"id":"response","choices":[{"message":{"role":"assistant","content":"done","reasoning_content":"original","vendor_extension":{"signature":"opaque"}},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1},"vendor_metadata":{"version":2}}`, true},
		{"provider error", 400, `{"error":{"code":"invalid_request_error","message":"missing reasoning"}}`, false},
		{"malformed response", 200, `not JSON`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := testkit.RequireFactory(t, "agent/model", newContractFixture)
			c := f.Client.(*client)
			c.options.Observe = nil
			calls := 0
			c.options.HTTPClient = &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			ctx := testkit.Context(t)
			stream, err := c.Generate(ctx, f.Request)
			if tc.success {
				testkit.RequireNoError(t, err)
				testkit.RequireNoError(t, stream.Close())
			} else if err == nil {
				t.Fatal("invalid response succeeded")
			}
			rows, err := persistence.Select[json.RawMessage](ctx, c.store, "agent.model_attempts", persistence.Query{})
			testkit.RequireNoError(t, err)
			if len(rows) != 1 || strings.Contains(string(rows[0]), `"Response"`) || strings.Contains(string(rows[0]), `"Deltas"`) || strings.Contains(string(rows[0]), "vendor_extension") {
				t.Fatal("model output persisted", rows)
			}
			restarted := NewClient(c.store, c.options)
			_, err = restarted.Generate(ctx, f.Request)
			if err == nil || calls != 1 {
				t.Fatal("existing attempt must not replay or redispatch", err, calls)
			}

		})
	}
}
