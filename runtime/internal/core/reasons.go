package core

import "errors"

// Reason describes a user-actionable failure without publishing private errors.
// Codes continue to determine HTTP and scheduler behavior.
type Reason string

const (
	AssistantBudgetMissing       Reason = "assistant_budget_missing"
	ModelNotConfigured           Reason = "model_not_configured"
	ModelUnavailable             Reason = "model_unavailable"
	ModelNotAuthorized           Reason = "model_not_authorized"
	ModelPricingMissing          Reason = "model_pricing_missing"
	ModelAuthenticationFailed    Reason = "model_authentication_failed"
	ModelRequestRejected         Reason = "model_request_rejected"
	ModelQuotaExceeded           Reason = "model_quota_exceeded"
	ModelRetryExhausted          Reason = "model_retry_exhausted"
	ModelBindingChanged          Reason = "model_binding_changed"
	ModelRateLimited             Reason = "model_rate_limited"
	ModelServiceUnavailable      Reason = "model_service_unavailable"
	ModelContextIncompatible     Reason = "model_context_incompatible"
	NetworkAccessDenied          Reason = "network_access_denied"
	ContextLimit                 Reason = "context_limit"
	ContextSummaryInputTooLarge  Reason = "context_summary_input_too_large"
	ContextSummaryTruncated      Reason = "context_summary_truncated"
	ContextSummaryInvalid        Reason = "context_summary_invalid"
	ContextCompactionIneffective Reason = "context_compaction_ineffective"
	UnsupportedAttachment        Reason = "unsupported_attachment"
)

func WithReason(err error, reason Reason) error {
	if err == nil || ReasonOf(err) != "" {
		return err
	}
	return &Error{Code: CodeOf(err), Message: "operation cannot proceed", Reason: reason, cause: err}
}

func ReasonOf(err error) Reason {
	for err != nil {
		var detail *Error
		if !errors.As(err, &detail) {
			return ""
		}
		if detail.Reason != "" {
			return detail.Reason
		}
		err = errors.Unwrap(detail)
	}
	return ""
}
