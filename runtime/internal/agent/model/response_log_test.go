package model

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/tests/testkit"
)

func TestResponseLogCapturesHTTPAndSSEWithoutAffectingExecution(t *testing.T) {
	for _, body := range []string{
		`{"id":"response","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
		"data: broken\n\n",
	} {
		t.Run(body[:4], func(t *testing.T) {
			f := newContractFixture(t)
			c := f.Client.(*client)
			directory := filepath.Join(t.TempDir(), "responses")
			logger := NewResponseLogger(directory)
			if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("created log before first response")
			}
			c.options.ResponseLog = logger.Record
			c.options.HTTPClient = &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
				contentType := "application/json"
				if strings.HasPrefix(body, "data:") {
					contentType = "text/event-stream"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			stream, err := c.Generate(t.Context(), f.Request)
			if strings.HasPrefix(body, "{") {
				testkit.RequireNoError(t, err)
				stream.Close()
			} else if err == nil {
				t.Fatal("malformed SSE accepted")
			}
			raw, err := os.ReadFile(filepath.Join(directory, "responses.jsonl"))
			testkit.RequireNoError(t, err)
			var record RawResponse
			testkit.RequireNoError(t, json.Unmarshal(raw, &record))
			if record.Body != body || record.Status != 200 || record.Incomplete != strings.HasPrefix(body, "data:") {
				t.Fatalf("raw response changed: %+v", record)
			}
		})
	}
	f := newContractFixture(t)
	c := f.Client.(*client)
	c.options.ResponseLog = func(Request, RawResponse) error { return errors.New("disk full") }
	stream, err := c.Generate(t.Context(), f.Request)
	testkit.RequireNoError(t, err)
	stream.Close()
	receipt, err := c.receipts.ByOperation(t.Context(), f.Owner.OwnerID, f.Request.Execution.OperationID)
	testkit.RequireNoError(t, err)
	if !receipt.OutcomeKnown || receipt.Charged == nil {
		t.Fatal("log failure lost billing evidence")
	}
}

func TestResponseLogRotationAndPermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "logs")
	logger := NewResponseLogger(directory)
	logger.maxBytes = 300
	for i := range 10 {
		err := logger.Record(Request{}, RawResponse{Status: 200, Body: strings.Repeat("x", 40) + string(rune('a'+i))})
		testkit.RequireNoError(t, err)
	}
	files, err := os.ReadDir(directory)
	testkit.RequireNoError(t, err)
	if len(files) != 3 {
		t.Fatal("rotation did not bound files", files)
	}
	for _, entry := range files {
		info, err := entry.Info()
		testkit.RequireNoError(t, err)
		if info.Mode().Perm() != 0600 || info.Size() > logger.maxBytes {
			t.Fatal("unsafe permissions or unbounded file", info)
		}
	}
	raw, err := os.ReadFile(filepath.Join(directory, "responses.jsonl"))
	testkit.RequireNoError(t, err)
	if !strings.Contains(string(raw), strings.Repeat("x", 40)+"j") {
		t.Fatal("latest response lost")
	}
}
