package sandboxmanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
)

type response struct {
	Data   json.RawMessage
	Code   core.Code
	Reason core.Reason
}

// Handler is never mounted on the Runtime's public API. A standalone service
// requires TLS plus a deployment-provisioned secret, with no browser origins.
func Handler(m *Manager, token core.Secret) (http.Handler, error) {
	if len(token.Reveal()) < 32 {
		return nil, core.Invalid("token", "at least 32 characters required")
	}
	gate := make(chan struct{}, 16)
	forwardGate := make(chan struct{}, 64)
	expected := sha256.Sum256([]byte(token.Reveal()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		provided := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
		if r.Header.Get("Origin") != "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(response{Code: core.Unauthenticated})
			return
		}
		if r.URL.Path == "/v2/sandbox/forward" {
			select {
			case forwardGate <- struct{}{}:
				defer func() { <-forwardGate }()
			default:
				http.Error(w, "forward connection limit", 429)
				return
			}
			m.forwardHTTP(w, r)
			return
		}
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(response{Code: core.RateLimited})
			return
		}
		var value any
		var err error
		if r.Method == http.MethodGet && r.URL.Path == "/v2/sandbox/info" {
			value, err = m.Backend.Info(r.Context())
		} else if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, MaxRequest)
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			decode := func(v any) error {
				if d.Decode(v) != nil {
					return core.Invalid("request", "invalid request")
				}
				var extra any
				if d.Decode(&extra) != io.EOF {
					return core.Invalid("request", "trailing data")
				}
				return nil
			}
			switch r.URL.Path {
			case "/v2/sandbox/quotas":
				var q Request
				if err = decode(&q); err == nil {
					if len(q.NetworkAuthorizations) > 32 {
						err = core.Invalid("quotas", "too many network authorizations")
					} else if _, err = m.Get(r.Context(), q.Identity); err == nil {
						q.Method = "quota_update"
						value, err = m.Backend.Call(r.Context(), q)
					}
				}
			case "/v2/sandbox/read":
				var q Request
				if err = decode(&q); err == nil {
					value, err = m.Read(r.Context(), q)
				}
			case "/v2/sandbox/submit":
				var q Request
				if err = decode(&q); err == nil {
					value, err = m.Submit(r.Context(), q)
				}
			case "/v2/sandbox/get", "/v2/sandbox/renew", "/v2/sandbox/cancel":
				var q Identity
				if err = decode(&q); err == nil {
					switch r.URL.Path {
					case "/v2/sandbox/get":
						value, err = m.Get(r.Context(), q)
					case "/v2/sandbox/renew":
						err = m.Renew(r.Context(), q)
					case "/v2/sandbox/cancel":
						err = m.Cancel(r.Context(), q)
					}
				}
			case "/v2/sandbox/revoke":
				var q struct {
					OwnerID core.ID
					Epoch   uint64
				}
				if err = decode(&q); err == nil {
					err = m.Revoke(r.Context(), q.OwnerID, q.Epoch)
				}
			default:
				err = core.NewError(core.NotFound, "unknown endpoint")
			}
		} else {
			err = core.NewError(core.NotFound, "unknown endpoint")
		}
		out := response{}
		if err != nil {
			out.Code = core.CodeOf(err)
			out.Reason = core.ReasonOf(err)
		} else {
			out.Data, _ = json.Marshal(value)
		}
		_ = json.NewEncoder(w).Encode(out)
	}), nil
}

type Client struct {
	Endpoint string
	Token    core.Secret
	HTTP     *http.Client
}

func NewClient(endpoint, fingerprint string, token core.Secret, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, core.Invalid("sandbox", "HTTPS origin and service token required")
	}
	if len(token.Reveal()) < 32 {
		return nil, core.Invalid("token", "service token required")
	}
	pin, err := hex.DecodeString(strings.ToLower(fingerprint))
	if err != nil || len(pin) != 32 {
		return nil, core.Invalid("fingerprint", "SHA-256 certificate fingerprint required")
	}
	base, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		return nil, core.Invalid("transport", "guarded transport required")
	}
	transport := base.Clone()
	// An explicit certificate pin is the trust anchor, including for privately
	// issued certificates. Never use unauthenticated TLS or trust-on-first-use.
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return core.WithReason(core.NewError(core.Forbidden, "certificate missing"), "sandbox_certificate_mismatch")
		}
		cert := state.PeerCertificates[0]
		sum := sha256.Sum256(cert.Raw)
		if subtle.ConstantTimeCompare(sum[:], pin) != 1 || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
			return core.WithReason(core.NewError(core.Forbidden, "sandbox certificate mismatch"), "sandbox_certificate_mismatch")
		}
		return nil
	}}
	return &Client{Endpoint: strings.TrimSuffix(endpoint, "/"), Token: token, HTTP: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) call(ctx context.Context, method string, input, output any) error {
	var body io.Reader
	verb := http.MethodGet
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if len(raw) > MaxRequest {
			return core.Invalid("request", "too large")
		}
		body = bytes.NewReader(raw)
		verb = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, verb, c.Endpoint+"/v2/sandbox/"+method, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token.Reveal())
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		reason := core.ReasonOf(err)
		if reason == "" {
			reason = "sandbox_unreachable"
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				reason = "sandbox_connection_timeout"
			}
			if core.CodeOf(err) == core.Forbidden {
				reason = "sandbox_destination_denied"
			}
		}
		return core.WithReason(core.NewError(core.Unavailable, "sandbox connection unavailable"), reason)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return core.WithReason(core.NewError(core.Unauthenticated, "sandbox authentication failed"), "sandbox_auth_failed")
	}
	if res.StatusCode == http.StatusNotFound && method == "info" {
		return core.WithReason(core.NewError(core.FailedPrecondition, "upgrade the sandbox service to protocol 2"), "sandbox_protocol_unsupported")
	}
	if res.StatusCode != 200 {
		return core.NewError(core.Unavailable, "sandbox rejected request")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxMessage+1))
	if err != nil || len(data) > MaxMessage {
		return core.NewError(core.Unavailable, "invalid sandbox response")
	}
	var reply response
	if json.Unmarshal(data, &reply) != nil {
		return core.NewError(core.Unavailable, "invalid sandbox response")
	}
	if reply.Code == core.NotFound && method == "info" {
		return core.WithReason(core.NewError(core.FailedPrecondition, "upgrade the sandbox service to protocol 2"), "sandbox_protocol_unsupported")
	}
	if reply.Code != "" {
		return core.WithReason(core.NewError(reply.Code, "sandbox operation rejected"), reply.Reason)
	}
	if output != nil {
		return json.Unmarshal(reply.Data, output)
	}
	return nil
}
func (c *Client) Info(ctx context.Context) (Info, error) {
	var out Info
	err := c.call(ctx, "info", nil, &out)
	if err == nil && out.Protocol != ProtocolVersion {
		err = core.WithReason(core.NewError(core.FailedPrecondition, "unsupported sandbox protocol"), "sandbox_protocol_unsupported")
	}
	return out, err
}
func (c *Client) Get(ctx context.Context, i Identity) (Reply, error) {
	var out Reply
	err := c.call(ctx, "get", i, &out)
	return out, err
}
func (c *Client) QuotaUsage(ctx context.Context, q Request) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.call(ctx, "read", q, &raw)
	return raw, err
}
func (c *Client) UpdateQuotas(ctx context.Context, q Request) error {
	return c.call(ctx, "quotas", q, nil)
}
func (c *Client) Cancel(ctx context.Context, i Identity) error { return c.call(ctx, "cancel", i, nil) }
func (c *Client) Revoke(ctx context.Context, owner core.ID, epoch uint64) error {
	return c.call(ctx, "revoke", struct {
		OwnerID core.ID
		Epoch   uint64
	}{owner, epoch}, nil)
}
func (c *Client) Execute(ctx context.Context, q Request, recheck func(context.Context) error) (json.RawMessage, error) {
	if _, err := c.Info(ctx); err != nil {
		return nil, err
	}
	var reply Reply
	if err := recheck(ctx); err != nil {
		return nil, err
	}
	if q.Method == "snapshot" || q.Method == "download" || q.Method == "shell_read" || q.Method == "shell_output" {
		var result json.RawMessage
		err := c.call(ctx, "read", q, &result)
		if err != nil {
			return nil, err
		}
		if err = recheck(ctx); err != nil {
			return nil, err
		}
		return result, nil
	}
	err := c.call(ctx, "submit", q, &reply)
	// A lost acknowledgement may mean execution already started. Query, never
	// resubmit an uncertain mutation under a fresh operation ID.
	if err != nil {
		if code := core.CodeOf(err); code != core.Unavailable && code != core.DeadlineExceeded && code != core.Canceled {
			return nil, err
		}
		reply, err = c.Get(ctx, q.Identity)
		if err != nil {
			return nil, err
		}
	}
	return c.wait(ctx, q.Identity, reply, recheck)
}

// Await resumes observation of an existing operation without submitting it again.
func (c *Client) Await(ctx context.Context, id Identity, recheck func(context.Context) error) (json.RawMessage, error) {
	if _, err := c.Info(ctx); err != nil {
		return nil, err
	}
	if err := recheck(ctx); err != nil {
		return nil, err
	}
	reply, err := c.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return c.wait(ctx, id, reply, recheck)
}
func (c *Client) wait(ctx context.Context, id Identity, reply Reply, recheck func(context.Context) error) (json.RawMessage, error) {
	var err error
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	renew := time.Now()
	for reply.State == "running" {
		select {
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.Cancel(cleanup, id)
			cancel()
			return nil, core.NewError(core.Canceled, "sandbox wait canceled")
		case <-ticker.C:
		}
		if time.Since(renew) > 3*time.Second {
			if err = recheck(ctx); err != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				_ = c.Cancel(cleanup, id)
				cancel()
				return nil, err
			}
			if err = c.call(ctx, "renew", id, nil); err != nil {
				return nil, err
			}
			renew = time.Now()
		}
		reply, err = c.Get(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if err = recheck(ctx); err != nil {
		return nil, err
	}
	if reply.State != "succeeded" {
		code := reply.Code
		if code == "" {
			code = core.FailedPrecondition
		}
		return nil, core.WithReason(core.NewError(code, "sandbox operation did not complete"), reply.Reason)
	}
	return reply.Result, nil
}
