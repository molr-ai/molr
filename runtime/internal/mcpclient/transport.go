package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
)

// A separate client per operation prevents account/session state from leaking.
// Credentials exist only during the broker callback; redirects and all network
// dialing remain controlled by the injected platform guard.
func (s *Service) session(ctx context.Context, p core.Principal, e core.Execution, c Connection, fn func(*mcp.ClientSession, core.Secret) error) error {
	if s.options.HTTPClient == nil {
		return core.NewError(core.Unavailable, "MCP transport unavailable")
	}
	client, err := s.options.HTTPClient(ctx, p, e, c.ID, c.Origin())
	if err != nil {
		return err
	}
	perform := func(secret core.Secret) error {
		transport := &boundedTransport{operationContext: ctx, base: client.Transport, endpoint: c.Endpoint, auth: c.Auth, header: c.Header, secret: secret, check: func() error {
			current, err := s.ConnectionAccess(ctx, p, c.ID)
			if err != nil {
				return err
			}
			if current.Version != c.Version {
				return conflict()
			}
			return nil
		}}
		if transport.base == nil {
			return core.NewError(core.Unavailable, "guarded MCP transport required")
		}
		guarded := *client
		guarded.Transport = transport
		guarded.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("MCP redirects are disabled") }
		sdk := mcp.NewClient(&mcp.Implementation{Name: "molr", Version: "0.1.0"}, &mcp.ClientOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
		session, err := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: c.Endpoint, HTTPClient: &guarded, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
		if err != nil {
			return core.NewError(core.Unavailable, "MCP connection failed; check endpoint, authentication and network policy")
		}
		defer session.Close()
		return fn(session, secret)
	}
	if c.Auth == "none" {
		return perform(core.Secret{})
	}
	if s.options.Broker == nil {
		return denied()
	}
	return s.options.Broker.WithSecret(ctx, credential.Access{Principal: p, Execution: e, ResourceID: c.ID, CredentialID: c.CredentialID, Origin: c.Origin()}, perform)
}

type boundedTransport struct {
	operationContext       context.Context
	base                   http.RoundTripper
	endpoint, auth, header string
	secret                 core.Secret
	check                  func() error
	mu                     sync.Mutex
	remaining              int64
	initialized            bool
}

func (t *boundedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != t.endpoint {
		return nil, denied()
	}
	if err := t.check(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := func() bool { return false }
	if t.operationContext != nil {
		stop = context.AfterFunc(t.operationContext, cancel)
	}
	cleanup := func() { stop(); cancel() }
	r = r.Clone(ctx)
	r.Header = r.Header.Clone()
	switch t.auth {
	case "bearer":
		r.Header.Set("Authorization", "Bearer "+t.secret.Reveal())
	case "header":
		r.Header.Set(t.header, t.secret.Reveal())
	}
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		cleanup()
		return nil, err
	}
	t.mu.Lock()
	if !t.initialized {
		t.remaining = 4 << 20
		t.initialized = true
	}
	t.mu.Unlock()
	resp.Body = &boundedBody{ReadCloser: resp.Body, transport: t, cleanup: cleanup}
	return resp, nil
}

type boundedBody struct {
	cleanup func()
	io.ReadCloser
	transport *boundedTransport
}

func (b *boundedBody) Close() error {
	if b.cleanup != nil {
		b.cleanup()
	}
	return b.ReadCloser.Close()
}
func (b *boundedBody) Read(p []byte) (int, error) {
	b.transport.mu.Lock()
	remaining := b.transport.remaining
	b.transport.mu.Unlock()
	if remaining <= 0 {
		return 0, errors.New("MCP response limit exceeded")
	}
	if int64(len(p)) > remaining+1 {
		p = p[:remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.transport.mu.Lock()
	b.transport.remaining -= int64(n)
	over := b.transport.remaining < 0
	b.transport.mu.Unlock()
	if over {
		return 0, errors.New("MCP response limit exceeded")
	}
	return n, err
}
func schema(raw json.RawMessage) (*jsonschema.Resolved, error) {
	if len(raw) > 32<<10 {
		return nil, invalid("schema exceeds 32 KiB")
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil || object["type"] != "object" {
		return nil, invalid("tool input must be an object schema")
	}
	nodes := 0
	var check func(any, int) error
	check = func(v any, depth int) error {
		nodes++
		if nodes > 2048 || depth > 24 {
			return invalid("schema nesting or size exceeds limit")
		}
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "$ref" || k == "$dynamicRef" || k == "x-mcp-header" {
					return invalid("schema references and parameter headers are not supported")
				}
				if err := check(v, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, v := range x {
				if err := check(v, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := check(object, 0); err != nil {
		return nil, err
	}
	var v jsonschema.Schema
	if json.Unmarshal(raw, &v) != nil {
		return nil, invalid("invalid JSON Schema")
	}
	resolved, err := v.Resolve(nil)
	if err != nil {
		return nil, invalid("unsupported or invalid JSON Schema")
	}
	return resolved, nil
}
func discover(ctx context.Context, session *mcp.ClientSession, c Connection) ([]Tool, error) {
	out := []Tool{}
	names := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	bytes := 0
	for page := 0; page < 16; page++ {
		result, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil || result == nil {
			return nil, core.NewError(core.Unavailable, "MCP tool discovery failed")
		}
		for _, remote := range result.Tools {
			if remote == nil || remote.Name == "" || len(remote.Name) > 128 || names[remote.Name] {
				return nil, invalid("MCP returned invalid or duplicate tool names")
			}
			names[remote.Name] = true
			raw, err := json.Marshal(remote.InputSchema)
			if err != nil {
				return nil, invalid("invalid MCP schema")
			}
			bytes += len(raw) + len(remote.Description) + len(remote.Name)
			if len(out) >= 128 || bytes > 512<<10 {
				return nil, invalid("MCP catalog exceeds 128 tools or 512 KiB")
			}
			// Definition identity changes require both review and a new assistant grant.
			identity, _ := json.Marshal([]any{c.ID, remote.Name, remote.Description, json.RawMessage(raw)})
			t := Tool{ID: core.ID("mcp_" + hash(identity)[:48]), Name: remote.Name, Description: remote.Description, InputSchema: raw}
			if _, err := schema(raw); err != nil {
				t.Problem = "unsupported_schema"
			}
			if len(t.Description) > 16<<10 {
				t.Problem = "description_too_large"
			}
			out = append(out, t)
		}
		if result.NextCursor == "" {
			sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
			return out, nil
		}
		if cursors[result.NextCursor] {
			return nil, invalid("MCP repeated a pagination cursor")
		}
		cursors[result.NextCursor] = true
		cursor = result.NextCursor
	}
	return nil, invalid("MCP catalog pagination exceeds limit")
}
func (s *Service) Refresh(ctx context.Context, p core.Principal, q Request) (View, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return View{}, err
	}
	if err := q.Mutation.Validate(true); err != nil {
		return View{}, err
	}
	c, err := s.ConnectionAccess(ctx, p, q.ID)
	if err != nil {
		return View{}, err
	}
	if c.Version != q.Mutation.ExpectedVersion {
		return View{}, conflict()
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	// Discovery is bounded separately from billable tool-call admission.
	s.mu.Lock()
	key := "refresh/" + string(c.ID)
	if _, exists := s.runs[key]; exists {
		s.mu.Unlock()
		return View{}, core.NewError(core.Conflict, "MCP discovery already running")
	}
	s.runs[key] = running{Call: CallRecord{Connection: c}, Principal: p, Cancel: cancel}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.runs, key); s.mu.Unlock() }()
	var found []Tool
	probeErr := s.session(runCtx, p, core.Execution{}, c, func(session *mcp.ClientSession, secret core.Secret) error {
		var err error
		found, err = discover(runCtx, session, c)
		if err == nil && !secret.Empty() {
			for _, t := range found {
				raw, _ := json.Marshal(t)
				var value any
				_ = json.Unmarshal(raw, &value)
				clean, _ := json.Marshal(redact(value, secret.Reveal()))
				var canonical any
				_ = json.Unmarshal(raw, &canonical)
				original, _ := json.Marshal(canonical)
				if string(clean) != string(original) {
					return core.NewError(core.FailedPrecondition, "MCP catalog contains credential material")
				}
			}
		}
		return err
	})
	persist, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelPersist()
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.store.Write(persist, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, true); err != nil {
			return err
		}
		current, err := s.connection(ctx, p.OwnerID, c.ID)
		if err != nil {
			return err
		}
		if current.Version != c.Version {
			return conflict()
		}
		old := current.Tools
		now := time.Now().UTC()
		current.CheckedAt = &now
		current.Status = "offline"
		if probeErr == nil {
			current.Status = "available"
			for i := range found {
				for _, t := range old {
					if t.ID == found[i].ID {
						found[i].Enabled = t.Enabled
					}
				}
			}
			current.Tools = found
		}
		current.Version++
		if err := s.syncResources(ctx, current, old); err != nil {
			return err
		}
		if err := tx.Put(ctx, "mcp.connections", persistence.Key(c.OwnerID, c.ID), current); err != nil {
			return err
		}
		c = current
		return s.audit(ctx, p, "refresh", c.ID)
	})
	if err != nil {
		return View{}, err
	}
	s.cancelLocked(c.ID)
	if probeErr != nil {
		return View{}, probeErr
	}
	return s.view(ctx, p, c, true)
}
