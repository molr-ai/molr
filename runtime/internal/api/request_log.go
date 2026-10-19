package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Observe API requests outside dispatch so rejected requests are measured too.
// Only registered route templates are logged; paths, queries and bodies may
// contain credentials or user content. Request IDs are always server-generated.
func (s *HTTPServer) observeRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
		s.dispatch(w, r, requestID())
		return
	}
	started := time.Now()
	id := requestID()
	_, route := s.mux.Handler(r)
	if route == "" {
		route = "unmatched"
	}
	method := r.Method
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
	default:
		method = "OTHER"
	}
	w.Header().Set("X-Request-ID", string(id))
	response := &timedResponseWriter{ResponseWriter: w, started: started}
	completed := false
	defer func() {
		// A handler with no body still produces a normal 200 response. Do not
		// commit a successful response while unwinding a panic.
		if completed && response.status == 0 {
			response.WriteHeader(http.StatusOK)
		}
		s.options.RequestLogger.Info("http_request",
			"request_id", id, "method", method, "route", route,
			"status", response.status, "duration_ms", milliseconds(time.Since(started)),
			"header_ms", milliseconds(response.headerDuration), "bytes", response.bytes,
			"stream", response.stream, "aborted", !completed,
			"canceled", r.Context().Err() != nil, "write_error", response.writeError)
	}()
	s.dispatch(response, r, id)
	completed = true
}

func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// Unwrap preserves ResponseController deadlines and transport capabilities.
// FlushError intercepts the implicit response headers used by SSE, without
// buffering responses or waiting for a stream to finish to publish timing.
type timedResponseWriter struct {
	http.ResponseWriter
	started        time.Time
	headerDuration time.Duration
	status         int
	bytes          int64
	stream         bool
	writeError     bool
}

func (w *timedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *timedResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.headerDuration = time.Since(w.started)
	w.stream = strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream")
	w.Header().Set("Server-Timing", fmt.Sprintf("runtime;dur=%.3f;desc=\"Time to response headers\"", milliseconds(w.headerDuration)))
	w.ResponseWriter.WriteHeader(status)
}

func (w *timedResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	w.writeError = w.writeError || err != nil
	return n, err
}

func (w *timedResponseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	err := http.NewResponseController(w.ResponseWriter).Flush()
	w.writeError = w.writeError || err != nil
	return err
}
