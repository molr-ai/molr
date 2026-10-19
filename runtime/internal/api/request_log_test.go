package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestDiagnostics(t *testing.T) {
	f := fixture(t)
	s := f.Server.(*HTTPServer)
	var logs bytes.Buffer
	s.options.RequestLogger = slog.New(slog.NewJSONHandler(&logs, nil))
	for _, tc := range []struct {
		name, path, route string
		status            int
		stripAuth         bool
	}{
		{"success", "/api/models?search=private-query", "GET /api/models", 200, false},
		{"unauthenticated", "/api/models", "GET /api/models", 401, true},
		{"path redaction", "/api/assistants/private-object", "GET /api/assistants/{id}", 401, true},
		{"unmatched", "/api/private-unmatched", "unmatched", 404, false},
		{"credential rejection", "/api/models?token=private-token", "GET /api/models", 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			r := f.ReadRequest()
			u := httptest.NewRequest("GET", "https://molr.example"+tc.path, nil).URL
			r.URL = u
			r.Header.Set("X-Request-ID", "private-client-id")
			if tc.stripAuth {
				f.StripCredentials(r)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			response := w.Result()
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", response.StatusCode, tc.status)
			}
			id := response.Header.Get("X-Request-ID")
			if id == "" || id == "private-client-id" {
				t.Fatal("missing server request ID")
			}
			if !strings.HasPrefix(response.Header.Get("Server-Timing"), "runtime;dur=") {
				t.Fatal("missing response timing")
			}
			if response.Header.Get("Access-Control-Expose-Headers") != "Server-Timing, X-Request-ID" && tc.status != 400 {
				t.Fatal("diagnostic headers not exposed")
			}
			var entry struct {
				ID       string  `json:"request_id"`
				Route    string  `json:"route"`
				Status   int     `json:"status"`
				Duration float64 `json:"duration_ms"`
				Header   float64 `json:"header_ms"`
				Bytes    int     `json:"bytes"`
			}
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if entry.ID != id || entry.Route != tc.route || entry.Status != tc.status || entry.Duration < entry.Header || entry.Header <= 0 || entry.Bytes != w.Body.Len() {
				t.Fatalf("incorrect log: %s", logs.String())
			}
			if strings.Contains(logs.String(), "private-") || strings.Contains(logs.String(), "Cookie") {
				t.Fatal("private request data in log")
			}
			if tc.status != 200 && tc.status != 404 {
				var body ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || string(body.RequestID) != id {
					t.Fatal("error ID does not match header")
				}
			}
		})
	}
}

type diagnosticWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *diagnosticWriter) SetWriteDeadline(t time.Time) error { w.deadline = t; return nil }

func TestTimingPreservesStreamingAndDeadlines(t *testing.T) {
	underlying := &diagnosticWriter{ResponseRecorder: httptest.NewRecorder()}
	w := &timedResponseWriter{ResponseWriter: underlying, started: time.Now().Add(-25 * time.Millisecond)}
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(time.Second)
	if err := controller.SetWriteDeadline(deadline); err != nil || !underlying.deadline.Equal(deadline) {
		t.Fatal("deadline forwarding failed")
	}
	if err := controller.Flush(); err != nil {
		t.Fatal(err)
	}
	if !underlying.Flushed || !w.stream || w.status != 200 || w.headerDuration < 25*time.Millisecond {
		t.Fatal("stream did not flush immediately with timing")
	}
	header := underlying.Result().Header.Get("Server-Timing")
	if header == "" {
		t.Fatal("timing not committed at stream open")
	}
	if _, err := io.WriteString(w, "data: update\n\n"); err != nil {
		t.Fatal(err)
	}
	w.WriteHeader(500)
	if w.status != 200 || w.bytes != 14 || w.Header().Get("Server-Timing") != header {
		t.Fatal("stream writes changed committed status or timing")
	}
}

type failingDiagnosticWriter struct{ *httptest.ResponseRecorder }

func (w failingDiagnosticWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTimingWriteFailure(t *testing.T) {
	w := &timedResponseWriter{ResponseWriter: failingDiagnosticWriter{httptest.NewRecorder()}, started: time.Now()}
	_, err := w.Write([]byte("body"))
	if !errors.Is(err, io.ErrClosedPipe) || !w.writeError || w.bytes != 0 {
		t.Fatal("write failure lost")
	}
}

type panicLimiter struct{}

func (panicLimiter) Allow(context.Context, RateKey) (Allowance, error) { panic(http.ErrAbortHandler) }

func TestAbortedRequestIsLoggedWithoutCommittingSuccess(t *testing.T) {
	f := fixture(t)
	s := f.Server.(*HTTPServer)
	var logs bytes.Buffer
	s.options.RequestLogger = slog.New(slog.NewJSONHandler(&logs, nil))
	s.options.Limiter = panicLimiter{}
	w := httptest.NewRecorder()
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Error("panic was not propagated")
			}
		}()
		s.Handler().ServeHTTP(w, f.ReadRequest())
	}()
	if !strings.Contains(logs.String(), `"aborted":true`) || !strings.Contains(logs.String(), `"status":0`) || w.Header().Get("Server-Timing") != "" {
		t.Fatal("aborted request logged as completed", logs.String())
	}
}
