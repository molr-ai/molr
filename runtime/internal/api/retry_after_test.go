package api

import (
	"fmt"
	"molr.ai/runtime/internal/core"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticationRetryAfterHeader(t *testing.T) {
	for _, tc := range []struct {
		delay time.Duration
		want  string
	}{{0, ""}, {time.Nanosecond, "1"}, {time.Minute, "60"}, {time.Minute + time.Nanosecond, "61"}, {10 * time.Minute, "600"}} {
		t.Run(tc.want, func(t *testing.T) {
			w := httptest.NewRecorder()
			fail(w, fmt.Errorf("wrapped: %w", &core.Error{Code: core.RateLimited, RetryAfter: tc.delay}), "fixture")
			if w.Code != 429 || w.Header().Get("Retry-After") != tc.want {
				t.Fatalf("status %d, header %q", w.Code, w.Header().Get("Retry-After"))
			}
		})
	}
}
