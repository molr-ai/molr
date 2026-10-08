package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/quota"
)

type HTTPCall struct {
	ID              core.ID
	OwnerID         core.ID
	PluginID        string
	Digest          string
	ToolID          string
	ProviderID      string
	Connection      Connection
	SettingsVersion core.Version
	Settings        map[string]any
	Arguments       json.RawMessage
	Operation       string
	State           string
	InputDigest     string
	CreatedAt       time.Time
	FinishedAt      *time.Time
	DurationMS      int64
	StatusCode      int
	Result          json.RawMessage
	Failure         string
	Test            bool
}
type httpRun struct {
	Principal    core.Principal
	ToolName     string
	ConnectionID core.ID
	OwnerID      core.ID
	PluginID     string
	Cancel       context.CancelFunc
}

func (s *Service) connectionUsage(ctx context.Context, id core.ID) (int64, int, error) {
	used, err := quota.CallTotals(ctx, s.store, "connection", "", id, "", time.Time{}, time.Time{})
	return used.Calls, used.Active, err
}

// PrepareCall runs inside the executor's admission transaction, before any IO.
// Successful operation replay is handled by the executor before reaching here.
func (s *Service) PrepareCall(ctx context.Context, c tools.Call) (core.ID, error) {
	r, err := s.resolve(ctx, c)
	if err != nil {
		return "", err
	}
	if r.tool == nil || r.tool.Execution.Type != "http" {
		return "", nil
	}
	settings, err := s.executionSettings(ctx, c.Execution.Principal.OwnerID, r.installation, r.tool.Execution.SettingsScope)
	if err != nil {
		return "", err
	}
	if settings.NeedsReview {
		return "", core.NewError(core.FailedPrecondition, "plugin settings require review")
	}
	id, _ := settings.Values[r.tool.Execution.ConnectionSetting].(string)
	if id == "" {
		return "", core.NewError(core.FailedPrecondition, "select a plugin connection in plugin settings")
	}
	connection, err := s.usableConnection(ctx, c.Execution.Principal, r.installation, core.ID(id))
	if err != nil {
		return "", err
	}
	schema, _ := settingsSchema(r.installation.Manifest, r.tool.Execution.SettingsScope)
	f := schema[r.tool.Execution.ConnectionSetting]
	allowed := false
	for _, p := range f.Providers {
		if p == connection.ProviderID {
			allowed = true
		}
	}
	if !allowed {
		return "", invalid("selected provider is not supported")
	}
	record := HTTPCall{ID: c.Execution.OperationID, OwnerID: c.Execution.Principal.OwnerID, PluginID: r.installation.ID, Digest: r.installation.Digest, ToolID: r.tool.ID, ProviderID: connection.ProviderID, Connection: connection, SettingsVersion: settings.Version, Settings: settings.Values, Arguments: c.Arguments, Operation: r.tool.Execution.Operation, State: "prepared", InputDigest: hash(c.Arguments), CreatedAt: time.Now()}
	if err := s.reserveHTTP(ctx, c.Execution.Principal, record); err != nil {
		return "", err
	}
	return connection.ID, nil
}
func (s *Service) reserveHTTP(ctx context.Context, p core.Principal, r HTTPCall) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(r.OwnerID, r.ID)
		var old HTTPCall
		if err := tx.Get(ctx, "plugins.http-calls", key, &old); err == nil {
			return core.NewError(core.FailedPrecondition, "HTTP operation already admitted")
		} else if !persistence.Missing(err) {
			return err
		}
		calls, active, err := s.connectionUsage(ctx, r.Connection.ID)
		if err != nil {
			return err
		}
		if calls >= r.Connection.CallLimit || active >= r.Connection.Concurrent {
			return core.NewError(core.BudgetExceeded, "plugin connection call or concurrency limit reached")
		}
		if s.options.CheckConnectionQuota != nil {
			if err := s.options.CheckConnectionQuota(ctx, p, r.Connection.ID); err != nil {
				return err
			}
		}
		if err := tx.Put(ctx, "plugins.http-calls", key, r); err != nil {
			return err
		}
		return s.audit(ctx, p, "http.begin", r.PluginID)
	})
}

// Admission may have reserved a connection before cancellation or revocation
// prevents dispatch. runHTTP owns dispatched requests; this closes only the
// matching, still-prepared record, including when the caller's context ended.
func (s *Service) finishPreparedHTTP(ctx context.Context, c tools.Call) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(c.Execution.Principal.OwnerID, c.Execution.OperationID)
		r, err := persistence.Load[HTTPCall](ctx, tx.Store(), "plugins.http-calls", key)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if r.Test || r.State != "prepared" || r.InputDigest != hash(c.Arguments) {
			return nil
		}
		now := time.Now()
		r.State, r.Failure, r.FinishedAt = "failed", "not_dispatched", &now
		return tx.Put(ctx, "plugins.http-calls", key, r)
	})
}

func (s *Service) executeHTTP(ctx context.Context, c tools.Call) (json.RawMessage, error) {
	record, err := persistence.Load[HTTPCall](ctx, s.store, "plugins.http-calls", persistence.Key(c.Execution.Principal.OwnerID, c.Execution.OperationID))
	if err != nil {
		return nil, err
	}
	if record.Test || record.InputDigest != hash(c.Arguments) || record.State != "prepared" {
		return nil, core.NewError(core.FailedPrecondition, "HTTP operation cannot be replayed")
	}
	return s.runHTTP(ctx, c.Execution.Principal, c.Execution, record)
}

type TestConnectionRequest struct {
	PluginID, Digest string
	ID               core.ID
	Mutation         core.Mutation
}

func (s *Service) TestConnection(ctx context.Context, p core.Principal, q TestConnectionRequest) (json.RawMessage, error) {
	if err := s.authorize(ctx, p, false); err != nil {
		return nil, err
	}
	if err := q.Mutation.Validate(false); err != nil {
		return nil, err
	}
	v, err := s.active(ctx, p, q.PluginID, q.Digest)
	if err != nil {
		return nil, err
	}
	c, err := s.usableConnection(ctx, p, v, q.ID)
	if err != nil {
		return nil, err
	}
	provider, _ := providerFor(v.Manifest, c.ProviderID)
	if provider.Test == nil {
		return nil, core.NewError(core.FailedPrecondition, "provider has no test operation")
	}
	settings, err := s.executionSettings(ctx, p.OwnerID, v, provider.Test.SettingsScope)
	if err != nil {
		return nil, err
	}
	if settings.NeedsReview {
		return nil, core.NewError(core.FailedPrecondition, "plugin settings require review")
	}
	id := Key("probe/" + q.PluginID + "/" + q.Mutation.IdempotencyKey)
	record := HTTPCall{ID: id, OwnerID: p.OwnerID, PluginID: v.ID, Digest: v.Digest, ProviderID: c.ProviderID, Connection: c, SettingsVersion: settings.Version, Settings: settings.Values, Arguments: provider.Test.Arguments, Operation: provider.Test.Operation, State: "prepared", InputDigest: hash([]byte(string(c.ID) + "/" + v.Digest)), CreatedAt: time.Now(), Test: true}
	if old, err := persistence.Load[HTTPCall](ctx, s.store, "plugins.http-calls", persistence.Key(p.OwnerID, id)); err == nil {
		if old.InputDigest != record.InputDigest {
			return nil, core.NewError(core.Conflict, "test retry changed connection")
		}
		if old.State == "succeeded" {
			return old.Result, nil
		}
		return nil, core.NewError(core.FailedPrecondition, "test was already attempted; inspect its result before retrying")
	} else if !persistence.Missing(err) {
		return nil, err
	}
	if err := s.reserveHTTP(ctx, p, record); err != nil {
		return nil, err
	}
	return s.runHTTP(ctx, p, core.Execution{}, record)
}
func (s *Service) runHTTP(ctx context.Context, p core.Principal, e core.Execution, r HTTPCall) (out json.RawMessage, runErr error) {
	started := time.Now()
	sent := false
	defer func() {
		finished := time.Now()
		r.FinishedAt = &finished
		r.DurationMS = finished.Sub(started).Milliseconds()
		r.Result = out
		if runErr != nil {
			r.State = "failed"
			r.Failure = string(core.CodeOf(runErr))
			if sent && r.StatusCode == 0 {
				r.State = "unknown"
			}
		} else {
			r.State = "succeeded"
		}
		if runErr != nil && r.State == "failed" {
			runErr = core.WithReason(runErr, "plugin_http_failed")
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err := s.store.Write(persist, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "plugins.http-calls", persistence.Key(r.OwnerID, r.ID), r)
		})
		if err != nil && runErr == nil {
			out = nil
			runErr = err
		}
	}()
	s.mu.Lock()
	current, provider, err := s.ConnectionAccess(ctx, p, r.Connection.ID)
	if err == nil && current.Version != r.Connection.Version {
		err = core.NewError(core.Forbidden, "connection changed before request")
	}
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	op, ok := provider.Operations[r.Operation]
	if !ok {
		s.mu.Unlock()
		return nil, invalid("operation unavailable")
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(r.Connection.TimeoutSeconds)*time.Second)
	key := persistence.Key(r.OwnerID, r.ID)
	toolName := ""
	if !r.Test {
		toolName = "plg_" + hash([]byte(r.PluginID + "/" + r.Digest + "/tool/" + r.ToolID))[:48]
	}
	s.httpRunning[key] = httpRun{Principal: p, ToolName: toolName, ConnectionID: r.Connection.ID, OwnerID: p.OwnerID, PluginID: r.PluginID, Cancel: cancel}
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.httpRunning, key); s.mu.Unlock() }()
	r.State = "running"
	if err := s.store.Write(runCtx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "plugins.http-calls", key, r) }); err != nil {
		return nil, err
	}
	var args map[string]any
	if err := json.Unmarshal(r.Arguments, &args); err != nil {
		return nil, invalid("invalid arguments")
	}
	query := url.Values{}
	body := map[string]any{}
	value := func(v HTTPValue) (any, error) {
		if v.Value != nil {
			var out any
			err := json.Unmarshal(v.Value, &out)
			return out, err
		}
		source, name := args, v.Argument
		if v.Setting != "" {
			source, name = r.Settings, v.Setting
		}
		out, ok := source[name]
		if !ok {
			return nil, invalid("missing HTTP parameter")
		}
		return out, nil
	}
	for k, v := range op.Query {
		x, err := value(v)
		if err != nil {
			return nil, err
		}
		switch x.(type) {
		case string, bool, float64:
			query.Set(k, fmt.Sprint(x))
		default:
			return nil, invalid("query value must be scalar")
		}
	}
	for k, v := range op.Body {
		x, err := value(v)
		if err != nil {
			return nil, err
		}
		body[k] = x
	}
	endpoint := provider.Origin + op.Path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var encoded []byte
	if op.Method == "POST" {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	if len(encoded) > 64<<10 || len(endpoint) > 16<<10 {
		return nil, invalid("HTTP request exceeds limit")
	}
	if s.options.HTTPClient == nil {
		return nil, core.NewError(core.Unavailable, "HTTP executor unavailable")
	}
	client, err := s.options.HTTPClient(runCtx, p, e, r.Connection.ID, provider.Origin)
	if err != nil {
		return nil, err
	}
	// The injected client is itself bound to the connection and deployment policy.
	perform := func(secret core.Secret) error {
		current, _, err := s.ConnectionAccess(runCtx, p, r.Connection.ID)
		if err != nil {
			return err
		}
		if current.Version != r.Connection.Version {
			return core.NewError(core.Forbidden, "connection changed")
		}
		req, err := http.NewRequestWithContext(runCtx, op.Method, endpoint, bytes.NewReader(encoded))
		if err != nil {
			return invalid("invalid HTTP request")
		}
		req.Header.Set("Accept", "application/json")
		if op.Method == "POST" {
			req.Header.Set("Content-Type", "application/json")
		}
		switch provider.Auth.Type {
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+secret.Reveal())
		case "header":
			req.Header.Set(provider.Auth.Header, secret.Reveal())
		}
		sent = true
		resp, err := client.Do(req)
		if err != nil {
			return core.NewError(core.Unavailable, "plugin HTTP request failed or timed out")
		}
		defer resp.Body.Close()
		r.StatusCode = resp.StatusCode
		raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if err != nil {
			return core.NewError(core.Unavailable, "incomplete provider response")
		}
		if len(raw) > 1<<20 {
			return core.NewError(core.FailedPrecondition, "provider response exceeds limit")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return core.NewError(core.Unavailable, fmt.Sprintf("provider returned HTTP %d", resp.StatusCode))
		}
		var data any
		if json.Unmarshal(raw, &data) != nil {
			return core.NewError(core.FailedPrecondition, "provider returned invalid JSON")
		}
		if op.ErrorField != "" {
			v, ok := pointer(data, op.ErrorField)
			if ok && applicationError(v) {
				return core.NewError(core.FailedPrecondition, "provider reported an application error")
			}
		}
		result, err := project(op.Result, data, false)
		if err != nil {
			return err
		}
		var usage any
		if op.Usage != nil {
			usage, err = project(*op.Usage, data, false)
			if err != nil {
				return err
			}
		}
		out, err = json.Marshal(map[string]any{"data": redactProjection(result, secret.Reveal()), "provider": r.ProviderID, "connection_id": r.Connection.ID, "usage": redactProjection(usage, secret.Reveal()), "duration_ms": time.Since(started).Milliseconds()})
		if err == nil && len(out) > 16<<10 {
			return core.NewError(core.FailedPrecondition, "projected result exceeds 16 KiB")
		}
		return err
	}
	if provider.Auth.Type == "none" {
		err = perform(core.Secret{})
	} else {
		if s.options.Broker == nil {
			return nil, core.NewError(core.Unavailable, "credential broker unavailable")
		}
		access := credential.Access{Principal: p, Execution: e, ResourceID: r.Connection.ID, CredentialID: r.Connection.CredentialID, Origin: provider.Origin}
		err = s.options.Broker.WithSecret(runCtx, access, perform)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
func pointer(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[part]
			if !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			value = v[i]
		default:
			return nil, false
		}
	}
	return value, true
}
func project(p Projection, input any, root bool) (any, error) {
	budget := 512
	bytes := 16 << 10
	return projectBudget(p, input, root, &budget, &bytes)
}
func projectBudget(p Projection, input any, root bool, budget, bytes *int) (any, error) {
	*budget--
	if *budget < 0 {
		return nil, core.NewError(core.FailedPrecondition, "projection exceeds work limit")
	}

	value, ok := input, true
	if !root {
		value, ok = pointer(input, p.Path)
	}
	if !ok || value == nil {
		if p.Default == nil {
			return nil, core.NewError(core.FailedPrecondition, "provider response missing required field")
		}
		if json.Unmarshal(p.Default, &value) != nil {
			return nil, invalid("invalid projection default")
		}
	}
	switch p.Type {
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return nil, core.NewError(core.FailedPrecondition, "expected response object")
		}
		out := map[string]any{}
		for key, field := range p.Fields {
			x, err := projectBudget(field, value, false, budget, bytes)
			if err != nil {
				return nil, err
			}
			out[key] = x
		}
		return out, nil
	case "array":
		values, ok := value.([]any)
		if !ok || p.Items == nil {
			return nil, core.NewError(core.FailedPrecondition, "expected response array")
		}
		out := []any{}
		for i, x := range values {
			if i >= p.MaxItems {
				break
			}
			v, err := projectBudget(*p.Items, x, false, budget, bytes)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case "string":
		if v, ok := value.(string); ok && len(v) <= 8192 {
			*bytes -= len(v)
			if *bytes < 0 {
				return nil, core.NewError(core.FailedPrecondition, "projection exceeds text limit")
			}
			return v, nil
		}
	case "number":
		if v, ok := value.(float64); ok {
			return v, nil
		}
	case "boolean":
		if v, ok := value.(bool); ok {
			return v, nil
		}
	}
	return nil, core.NewError(core.FailedPrecondition, "provider result does not match its contract")
}
func (s *Service) CancelOperation(owner, id core.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.httpRunning[persistence.Key(owner, id)]; ok {
		r.Cancel()
		return true
	}
	return false
}

func applicationError(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	default:
		return true
	}
}

// Recheck in-flight authority after account/group changes. Do not hold admission
// locks while reading the auth store, or call this from a store transaction.
func (s *Service) RevalidateRunning(ctx context.Context) {
	s.mu.Lock()
	runs := []httpRun{}
	for _, r := range s.httpRunning {
		runs = append(runs, r)
	}
	type shellRun struct {
		p      core.Principal
		id     string
		cancel context.CancelFunc
	}
	shells := []shellRun{}
	for key, active := range s.running {
		for _, cancel := range active {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) == 2 {
				shells = append(shells, shellRun{s.shellPrincipals[key], parts[1], cancel})
			}
		}
	}
	s.mu.Unlock()
	for _, r := range shells {
		v, err := s.load(ctx, r.p.OwnerID, r.id)
		if err == nil {
			err = s.available(ctx, r.p, v)
		}
		if err != nil {
			r.cancel()
		}
	}
	for _, r := range runs {
		_, _, err := s.ConnectionAccess(ctx, r.Principal, r.ConnectionID)
		if err == nil && r.ToolName != "" && s.options.CheckTool != nil {
			err = s.options.CheckTool(ctx, r.Principal, r.ToolName)
		}
		if err != nil {
			r.Cancel()
		}
	}
}
func (s *Service) RecoverHTTP(ctx context.Context) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := persistence.Select[HTTPCall](ctx, s.store, "plugins.http-calls", persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("State", "prepared")}, {persistence.Eq("State", "running")}}})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.State == "prepared" || r.State == "running" {
				r.State = "unknown"
				r.Failure = "runtime_restarted"
				if err := tx.Put(ctx, "plugins.http-calls", persistence.Key(r.OwnerID, r.ID), r); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func redactProjection(v any, secret string) any {
	if secret == "" {
		return v
	}
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, secret, "[redacted]")
	case map[string]any:
		for k, v := range x {
			x[k] = redactProjection(v, secret)
		}
	case []any:
		for i, v := range x {
			x[i] = redactProjection(v, secret)
		}
	}
	return v
}

func (s *Service) executionSettings(ctx context.Context, owner core.ID, v Installation, scope string) (UserSettings, error) {
	schema, err := settingsSchema(v.Manifest, scope)
	if err != nil {
		return UserSettings{}, err
	}
	v.Manifest.Settings = schema
	if scope == "admin" {
		owner = v.OwnerID
	}
	return s.settingsFor(ctx, owner, v, scope)
}
