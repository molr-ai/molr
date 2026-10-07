package core

import (
	"context"
	"errors"
	"time"
)

type Code string

const (
	InvalidArgument    Code = "invalid_argument"
	Unauthenticated    Code = "unauthenticated"
	Forbidden          Code = "forbidden"
	NotFound           Code = "not_found"
	Conflict           Code = "conflict"
	FailedPrecondition Code = "failed_precondition"
	RateLimited        Code = "rate_limited"
	BudgetExceeded     Code = "budget_exceeded"
	Unavailable        Code = "unavailable"
	Canceled           Code = "canceled"
	DeadlineExceeded   Code = "deadline_exceeded"
	Internal           Code = "internal"
)

// Error carries a safe diagnostic and optional private cause. HTTP adapters
// must map the Code to a fixed public message rather than expose Error().
type Error struct {
	Code       Code
	Message    string
	Field      string
	Reason     Reason
	RetryAfter time.Duration
	cause      error
}

func (e *Error) Error() string                  { return string(e.Code) + ": " + e.Message }
func (e *Error) Unwrap() error                  { return e.cause }
func NewError(code Code, message string) *Error { return &Error{Code: code, Message: message} }
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}
func Invalid(field, message string) *Error {
	return &Error{Code: InvalidArgument, Message: message, Field: field}
}
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return DeadlineExceeded
	}
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return Internal
}
