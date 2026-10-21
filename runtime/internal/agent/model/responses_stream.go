package model

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"strings"
	"time"
)

func readResponsesStream(reader io.Reader, started time.Time, now func() time.Time) (raw, completion []byte, timing *Timing, resultErr error) {
	var captured bytes.Buffer
	scanner := bufio.NewScanner(io.TeeReader(io.LimitReader(reader, (8<<20)+1), &captured))
	scanner.Buffer(make([]byte, 4096), (8<<20)+1)
	timing = &Timing{}
	defer func() {
		raw = append([]byte(nil), captured.Bytes()...)
		timing.RequestMs = max(0, float64(now().Sub(started))/float64(time.Millisecond))
	}()
	invalid := func() error { return core.NewError(core.Unavailable, "invalid or incomplete Responses stream") }
	done := false
	data := []string{}
	var first, last time.Time
	process := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		if done {
			return invalid()
		}
		var e struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
			Code     string          `json:"code"`
		}
		if json.Unmarshal([]byte(payload), &e) != nil {
			return invalid()
		}
		switch e.Type {
		case "response.output_text.delta", "response.function_call_arguments.delta":
			last = now()
			if first.IsZero() {
				first = last
				ms := max(0, float64(first.Sub(started))/float64(time.Millisecond))
				timing.FirstTokenMs = &ms
			}
		case "response.completed":
			var r responsesResponse
			if json.Unmarshal(e.Response, &r) != nil || r.Status != "completed" {
				return invalid()
			}
			completion = e.Response
			done = true
		case "response.failed":
			var r struct {
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal(e.Response, &r) != nil {
				return invalid()
			}
			body, _ := json.Marshal(map[string]any{"error": r.Error})
			return responsesFailure(body, now())
		case "error":
			return responsesFailure([]byte(`{"error":`+payload+`}`), now())
		case "response.incomplete":
			return invalid()
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := process(); err != nil {
				return nil, nil, timing, err
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, timing, invalid()
	}
	if err := process(); err != nil {
		return nil, nil, timing, err
	}
	if !done || captured.Len() > 8<<20 {
		return nil, nil, timing, invalid()
	}
	if !first.IsZero() {
		ms := max(0, float64(last.Sub(first))/float64(time.Millisecond))
		timing.GenerationMs = &ms
	}
	return nil, completion, timing, nil
}
func responsesFailure(body []byte, now time.Time) error {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	status := 500
	switch e.Error.Code {
	case "subscription_sharing_usage_limit_exceeded":
		status = 429
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
		status = 503
	case "subscription_sharing_user_not_eligible", "subscription_sharing_route_not_supported", "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context":
		status = 403
	case "subscription_sharing_invalid_user":
		status = 401
	case "subscription_sharing_unsupported_capability":
		status = 400
	}
	f := providerFailure(status, body, "", now)
	// A failed stream can follow partial generation; never assert zero usage.
	f.NoCharge = false
	f.OutcomeKnown = false
	return f
}
