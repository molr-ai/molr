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

func TestRawModelResponsesSurviveWithoutDebugCapture(t *testing.T) {
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
			cache, err := persistence.Load[cachedResult](ctx, c.store, "agent.model_requests", persistence.Key(f.Owner.OwnerID, f.Request.Execution.OperationID))
			testkit.RequireNoError(t, err)
			if cache.Response == nil || cache.Response.Status != tc.status || cache.Response.Body != tc.body {
				t.Fatal("full original response not durably retained")
			}
			// A fresh client reuses completed state and does not redispatch errors.
			restarted := NewClient(c.store, c.options)
			stream, err = restarted.Generate(ctx, f.Request)
			if tc.success {
				testkit.RequireNoError(t, err)
				defer stream.Close()
				delta, err := stream.Next(ctx)
				testkit.RequireNoError(t, err)
				var message map[string]json.RawMessage
				if delta.ProviderState == nil {
					t.Fatal("continuation message missing after restart")
				}
				testkit.RequireNoError(t, json.Unmarshal(delta.ProviderState.Payload, &message))
				if string(message["reasoning_content"]) != `"original"` || string(message["vendor_extension"]) != `{"signature":"opaque"}` {
					t.Fatal("unknown continuation fields lost")
				}
			} else if err == nil {
				t.Fatal("failed response was replayed as a completion")
			}
			if calls != 1 {
				t.Fatalf("response replay dispatched %d requests", calls)
			}
		})
	}
}
