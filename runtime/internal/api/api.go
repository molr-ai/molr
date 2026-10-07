// Package api defines HTTP composition, explicit routes and safe error translation.
package api

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/search"
	"molr.ai/runtime/internal/storage/files"
)

// Services is the owner-facing surface. It intentionally excludes bootstrap,
// raw credential access, Agent execution, arbitrary checkpoint mutation, provider bindings and repositories.
// Each service still rechecks policy; HTTP authentication is not sufficient.
type Services struct {
	Auth          auth.Service
	Browser       auth.BrowserGuard
	Audit         audit.Reader
	Assistants    assistant.Service
	Conversations conversation.Client
	Tasks         task.Service
	Todos         todo.Service
	Schedules     schedule.Service
	Resources     resource.Service
	Memories      memory.Service
	Settings      settings.Service
	Models        model.Catalog
	Credentials   credential.Manager
	Grants        policy.Grants
	Budget        policy.UsageReader
	Agent         agent.Observer
	Jobs          scheduler.Control
	Operations    operation.Control
	Search        search.Service
	Index         search.Maintenance
	Files         files.Service
	Realtime      realtime.Feed
	Exchange      pricing.ExchangeReader
	Debug         debugtrace.Control
	Dreams        dream.Service
	Plans         plan.Service
}

// Server uses configured TLS or a trusted proxy and applies authentication,
// CSRF, size/time/rate limits and origin checks before invoking services.
// Serve blocks until shutdown/context cancellation; Shutdown drains requests
// and closes SSE streams. Construction validates the complete fixed service composition.
type Server interface {
	Handler() http.Handler
	Serve(context.Context) error
	Shutdown(context.Context) error
}
type RequestInfo struct {
	ID            core.ID
	ClientAddress string
	Origin        string
	Secure        bool
}
type RateKey struct {
	Category string
	Account  string
	Address  string
}
type Allowance struct {
	Allowed    bool
	RetryAfter time.Duration
}

// RateLimiter failures deny access. Pre-auth login limits cannot rely on an
// authenticated account or trust unvalidated proxy source headers.
type RateLimiter interface {
	Allow(context.Context, RateKey) (Allowance, error)
}

// ErrorResponse contains only fixed messages, never raw wrapped errors or fields.
type ErrorResponse struct {
	Code      core.Code `json:"code"`
	Message   string    `json:"message"`
	RequestID core.ID   `json:"request_id,omitempty"`
}

func PublicError(err error, requestID core.ID) (int, ErrorResponse) {
	code := core.CodeOf(err)
	status, message := http.StatusInternalServerError, "Internal server error"
	switch code {
	case core.InvalidArgument:
		status, message = http.StatusBadRequest, "Invalid request"
	case core.Unauthenticated:
		status, message = http.StatusUnauthorized, "Authentication required"
	case core.Forbidden:
		status, message = http.StatusForbidden, "Access denied"
	case core.NotFound:
		status, message = http.StatusNotFound, "Not found"
	case core.Conflict:
		status, message = http.StatusConflict, "Conflict"
	case core.FailedPrecondition:
		status, message = http.StatusConflict, "Precondition failed"
	case core.RateLimited:
		status, message = http.StatusTooManyRequests, "Too many requests"
	case core.BudgetExceeded:
		status, message = http.StatusForbidden, "Budget exceeded"
	case core.Unavailable:
		status, message = http.StatusServiceUnavailable, "Service unavailable"
	case core.Canceled:
		status, message = http.StatusRequestTimeout, "Request canceled"
	case core.DeadlineExceeded:
		status, message = http.StatusGatewayTimeout, "Request timed out"
	default:
		code = core.Internal
	}
	if requestID.Validate() != nil {
		requestID = ""
	}
	return status, ErrorResponse{Code: code, Message: message, RequestID: requestID}
}

// Validate rejects missing and typed-nil services before binding a listener.
func (s Services) Validate() error {
	value := reflect.ValueOf(s)
	typ := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.IsNil() {
			return core.Invalid("services."+typ.Field(i).Name, "required")
		}
		actual := field.Elem()
		switch actual.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if actual.IsNil() {
				return core.Invalid("services."+typ.Field(i).Name, "required")
			}
		}
	}
	return nil
}
