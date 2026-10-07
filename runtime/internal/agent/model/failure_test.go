package model

import (
	"errors"
	"fmt"
	"io"
	"molr.ai/runtime/internal/core"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProviderFailureSeparatesRetrySafetyAndQuota(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		retry, known bool
	}{
		{"rate", 429, `{"error":{"code":"rate_limit_exceeded"}}`, true, true},
		{"quota", 429, `{"error":{"code":"insufficient_quota"}}`, false, true},
		{"auth", 401, `secret`, false, true}, {"invalid", 400, `bad`, false, true},
		{"gateway", 504, ``, false, false}, {"server", 500, ``, false, false},
		{"overload", 503, `{"error":{"type":"overloaded_error"}}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := providerFailure(tc.status, []byte(tc.body), "", time.Now())
			if f.Retryable != tc.retry || f.OutcomeKnown != tc.known || f.NoCharge != tc.known {
				t.Fatalf("unexpected classification: %+v", f)
			}
		})
	}
}
func TestRetryAfterFormatsAndInvalidValues(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"3", 3 * time.Second}, {now.Add(time.Minute).Format(http.TimeFormat), time.Minute},
		{"-1", 0}, {"abc", 0}, {"18446744073709551616", 0}, {now.Add(-time.Minute).Format(http.TimeFormat), 0},
	} {
		if got := retryAfter(tc.value, now); got != tc.want {
			t.Fatalf("%q: %s", tc.value, got)
		}
	}
}
func TestOnlyProvenPreSendNetworkErrorsRetry(t *testing.T) {
	for _, tc := range []struct {
		err  error
		safe bool
	}{
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{&net.OpError{Op: "read", Err: errors.New("reset")}, false},
		{errors.New("timeout after sending"), false},
	} {
		f := transportFailure(tc.err)
		if f.Retryable != tc.safe || f.NoCharge != tc.safe {
			t.Fatal(f)
		}
	}
}

func TestFailureRetainsTypedReasonThroughWrappers(t *testing.T) {
	f := providerFailure(429, []byte(`{}`), "3", time.Now())
	wrapped := fmt.Errorf("attempt failed: %w", f)
	if FailureOf(wrapped) != f || core.CodeOf(wrapped) != core.RateLimited || core.ReasonOf(wrapped) != core.ModelRateLimited || !f.KnownFailure() || f.Error() == "" || core.CodeOf(f.Unwrap()) != core.RateLimited {
		t.Fatal("lost failure evidence")
	}
	if FailureOf(nil) != nil {
		t.Fatal("nil became failure")
	}
}

type failureTransport func(*http.Request) (*http.Response, error)

func (f failureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRetryBindingRejectsChangedRequestBeforeDispatch(t *testing.T) {
	f := newContractFixture(t)
	c := f.Client.(*client)
	calls := 0
	c.options.HTTPClient = &http.Client{Transport: failureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})}
	req := f.Request
	req.LogicalID = "logical"
	req.Attempt = 1
	_, err := c.Generate(t.Context(), req)
	if failure := FailureOf(err); failure == nil || !failure.NoCharge {
		t.Fatal("rejection evidence missing", err)
	}
	receipt, err := c.receipts.ByOperation(t.Context(), req.Execution.Principal.OwnerID, req.Execution.OperationID)
	if err != nil || !receipt.OutcomeKnown || receipt.Charged == nil || receipt.Charged.Micros != 0 {
		t.Fatal("no-charge receipt missing", receipt, err)
	}
	req.Attempt = 2
	req.Execution.OperationID = "attempt-two"
	req.Inputs = []Input{{Role: User, Text: "changed request"}}
	_, err = c.Generate(t.Context(), req)
	if core.ReasonOf(err) != core.ModelBindingChanged || calls != 1 {
		t.Fatal("changed request dispatched", err, calls)
	}
}
