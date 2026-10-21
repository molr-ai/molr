package model

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
)

// Failure is durable evidence about one transport attempt, not authorization to
// repeat a turn. Unknown outcomes do not prove that the provider charged zero.
type Failure struct {
	Code         core.Code
	Reason       core.Reason
	Status       int
	ProviderCode string `json:",omitempty"`
	Retryable    bool
	OutcomeKnown bool
	NoCharge     bool
	RetryAfter   time.Duration
}

func (f *Failure) Error() string { return "model request failed: " + string(f.Reason) }
func (f *Failure) Unwrap() error {
	return core.WithReason(core.NewError(f.Code, "model request failed"), f.Reason)
}
func (f *Failure) KnownFailure() bool { return f.OutcomeKnown }
func FailureOf(err error) *Failure    { var f *Failure; errors.As(err, &f); return f }

func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if n, err := strconv.ParseUint(value, 10, 32); err == nil {
		return time.Duration(n) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(0, at.Sub(now))
	}
	return 0
}
func providerFailure(status int, body []byte, header string, now time.Time) *Failure {
	f := &Failure{Code: core.Unavailable, Reason: core.ModelServiceUnavailable, Status: status, RetryAfter: retryAfter(header, now)}
	var envelope struct {
		Error struct {
			Code json.RawMessage
			Type string
		}
	}
	_ = json.Unmarshal(body, &envelope)
	var code string
	_ = json.Unmarshal(envelope.Error.Code, &code)
	if code == "" {
		code = envelope.Error.Type
	}
	// Only recognized codes are retained in public diagnostics. Never store an
	// arbitrary provider message as a reason (it may contain private prompt data).
	switch code {
	case "subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable", "subscription_sharing_user_not_eligible", "subscription_sharing_route_not_supported", "subscription_sharing_invalid_user", "subscription_sharing_unsupported_capability", "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context", "context_length_exceeded", "context_window_exceeded", "prompt_too_long", "insufficient_quota", "quota_exceeded", "billing_hard_limit_reached", "invalid_api_key", "rate_limit_exceeded", "rate_limit_error", "overloaded_error", "server_overloaded":
		f.ProviderCode = code
	}
	switch status {
	case 400, 404, 405, 413, 422:
		f.Code, f.Reason, f.OutcomeKnown, f.NoCharge = core.InvalidArgument, core.ModelRequestRejected, true, true
	case 401, 403:
		f.Code, f.Reason, f.OutcomeKnown, f.NoCharge = core.Unauthenticated, core.ModelAuthenticationFailed, true, true
	case 429:
		f.Code, f.Reason, f.OutcomeKnown, f.NoCharge, f.Retryable = core.RateLimited, core.ModelRateLimited, true, true, true
		if code == "subscription_sharing_usage_limit_exceeded" || code == "insufficient_quota" || code == "quota_exceeded" || code == "billing_hard_limit_reached" {
			f.Reason, f.Retryable = core.ModelQuotaExceeded, false
		}
	case 503, 529:
		// A generic gateway failure does not prove that generation was rejected.
		if code == "subscription_sharing_usage_unavailable" || code == "subscription_sharing_user_unavailable" || code == "overloaded_error" || code == "server_overloaded" {
			f.OutcomeKnown, f.NoCharge, f.Retryable = true, true, true
		}
	}
	if f.Reason == core.ModelRequestRejected && (code == "context_length_exceeded" || code == "context_window_exceeded" || code == "prompt_too_long") {
		f.Reason = core.ModelContextExceeded
	}
	return f
}
func transportFailure(err error) *Failure {
	f := &Failure{Code: core.Unavailable, Reason: core.ModelServiceUnavailable}
	var op *net.OpError
	var dns *net.DNSError
	if errors.As(err, &op) && op.Op == "dial" {
		f.OutcomeKnown, f.NoCharge, f.Retryable = true, true, true
	}
	if errors.As(err, &dns) {
		f.OutcomeKnown, f.NoCharge, f.Retryable = true, true, dns.IsTemporary || dns.IsTimeout
	}
	return f
}
