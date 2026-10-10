package model

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"sync"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// Pricing uses integer millionths of currency per million tokens. A nil Pricing
// prohibits dispatch; a fully specified zero tariff explicitly denotes free use.
type Pricing struct {
	AudioInputMicrosPerMillion                                                 *int64 `json:",omitempty"`
	Currency                                                                   string
	InputMicrosPerMillion, OutputMicrosPerMillion, CachedInputMicrosPerMillion int64
}
type Provider struct {
	Protocol       Protocol
	Model          Model
	Endpoint       string
	CredentialID   core.ID
	Pricing        *Pricing
	ReservePricing *Pricing
}
type Options struct {
	// Dispatched records delivery only after a successful provider HTTP response.
	Dispatched       func(context.Context, Request) error
	PrepareInputs    func(context.Context, Request, Provider) ([]Input, error)
	Observe          func(context.Context, string, core.ID, any)
	ResponseLog      func(Request, RawResponse) error
	Operations       operation.Service
	Authorizer       policy.Authorizer
	ListModels       func(context.Context, core.Principal) ([]Model, error)
	Resolve          func(context.Context, core.Principal, core.ID) (Provider, error)
	CheckReservation func(context.Context, Request, Provider) error
	Settle           func(context.Context, core.Execution, Receipt) error
	Broker           credential.Broker
	// ClientFor must return a transport bound to the approved resource/origin and
	// connected IP, with DNS rebinding defense. HTTPClient is for trusted adapters
	// and local provider tests; CheckDestination is required with either path.
	ClientFor        func(context.Context, Request, Provider) (*http.Client, error)
	HTTPClient       *http.Client
	CheckDestination func(context.Context, core.Execution, core.ID, string) error
	Reconcile        func(context.Context, core.Execution, Receipt) (Receipt, error)
	Now              func() time.Time
}
type client struct {
	store    persistence.Store
	receipts Receipts
	options  Options
}

const DefaultRequestTimeout = 10 * time.Minute

func NewClient(store persistence.Store, options Options) Client {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &client{store, NewReceipts(store), options}
}
func (c *client) authorize(ctx context.Context, p core.Principal, resource, modelID core.ID, action policy.Action, destination string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if c.options.Authorizer == nil {
		return core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	err := c.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: resource}, ResourceID: resource, ModelID: modelID, Action: action, Destination: destination})
	if core.CodeOf(err) == core.Forbidden {
		return core.WithReason(err, core.ModelNotAuthorized)
	}
	return err
}
func (c *client) List(ctx context.Context, p core.Principal) ([]Model, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if c.options.Authorizer == nil || c.options.ListModels == nil {
		return nil, core.NewError(core.FailedPrecondition, "model catalog unavailable")
	}
	if err := c.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "model", ID: "catalog"}, Action: policy.Read}); err != nil {
		return nil, err
	}
	models, err := c.options.ListModels(ctx, p)
	if err != nil {
		return nil, err
	}
	allowed := []Model{}
	for _, model := range models {
		destination := ""
		if c.options.Resolve != nil {
			provider, resolveErr := c.options.Resolve(ctx, p, model.ID)
			if resolveErr != nil {
				if code := core.CodeOf(resolveErr); code == core.Forbidden || code == core.NotFound {
					continue
				}
				return nil, resolveErr
			}
			destination = provider.Endpoint
		}
		if err := c.authorize(ctx, p, model.ResourceID, model.ID, policy.Read, destination); err == nil {
			allowed = append(allowed, model)
		} else if code := core.CodeOf(err); code != core.Forbidden && code != core.NotFound {
			return nil, err
		}
	}
	return allowed, nil
}
func (c *client) Receipt(ctx context.Context, e core.Execution, id core.ID) (Receipt, error) {
	if err := e.Validate(); err != nil {
		return Receipt{}, err
	}
	if c.options.Authorizer == nil {
		return Receipt{}, core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	if err := c.options.Authorizer.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "operation", ID: e.OperationID}, Action: policy.Read}); err != nil {
		return Receipt{}, err
	}
	r, err := c.receipts.Get(ctx, e.Principal.OwnerID, id)
	if err != nil {
		return r, err
	}
	if r.OperationID != e.OperationID {
		return Receipt{}, core.NewError(core.Forbidden, "receipt belongs to another operation")
	}
	return r, nil
}
func (c *client) Reconcile(ctx context.Context, e core.Execution, id core.ID) (Receipt, error) {
	r, err := c.Receipt(ctx, e, id)
	if err != nil {
		return r, err
	}
	if !r.OutcomeKnown {
		if c.options.Reconcile == nil {
			return r, core.NewError(core.FailedPrecondition, "provider evidence unavailable")
		}
		known, err := c.options.Reconcile(ctx, e, r)
		if err != nil {
			return Receipt{}, err
		}
		if known.ID != r.ID || known.OperationID != r.OperationID {
			return Receipt{}, core.NewError(core.Conflict, "reconciliation identity mismatch")
		}
		if err = c.receipts.Record(ctx, e.Principal.OwnerID, known); err != nil {
			return Receipt{}, err
		}
		r = known
	}
	// Retry settlement even for an already-recorded receipt: the earlier write
	// may have succeeded just before settlement failed. Settlement is idempotent.
	if r.Charged != nil && c.options.Settle != nil && (r.Failure == nil || !r.Failure.NoCharge) {
		if err := c.options.Settle(ctx, e, r); err != nil {
			return r, err
		}
	}
	return r, nil
}

// attemptRecord retains only dispatch identity and failure metadata. Model output
// belongs to the checkpoint context; completed attempts cannot replay a result.
type attemptRecord struct {
	Failure   *Failure `json:",omitempty"`
	Digest    string
	Source    StateSource
	ReceiptID core.ID
}

func charge(pricing *Pricing, usage Usage) (*core.Money, error) {
	if pricing == nil {
		return nil, core.WithReason(core.NewError(core.FailedPrecondition, "explicit model pricing required"), core.ModelPricingMissing)
	}
	if err := (core.Money{Currency: pricing.Currency}).Validate(); err != nil {
		return nil, err
	}
	for _, n := range []int64{pricing.InputMicrosPerMillion, pricing.OutputMicrosPerMillion, pricing.CachedInputMicrosPerMillion, usage.InputTokens, usage.OutputTokens, usage.CachedTokens} {
		if n < 0 {
			return nil, core.Invalid("pricing", "negative usage or price")
		}
	}
	if usage.CachedTokens > usage.InputTokens {
		return nil, core.Invalid("usage", "invalid cached token count")
	}
	if usage.AudioInputTokens < 0 || usage.AudioInputTokens > usage.InputTokens {
		return nil, core.Invalid("usage", "invalid audio usage")
	}
	if usage.AudioInputTokens > 0 && (pricing.AudioInputMicrosPerMillion == nil || usage.CachedTokens > 0) {
		return nil, core.NewError(core.FailedPrecondition, "audio price or unambiguous cached audio usage required")
	}
	if pricing.AudioInputMicrosPerMillion != nil && *pricing.AudioInputMicrosPerMillion < 0 {
		return nil, core.Invalid("pricing", "negative audio price")
	}
	total := new(big.Int)
	if usage.AudioInputTokens > 0 {
		total.Mul(big.NewInt(usage.AudioInputTokens), big.NewInt(*pricing.AudioInputMicrosPerMillion))
	}
	for _, pair := range [][2]int64{{usage.InputTokens - usage.CachedTokens - usage.AudioInputTokens, pricing.InputMicrosPerMillion}, {usage.OutputTokens, pricing.OutputMicrosPerMillion}, {usage.CachedTokens, pricing.CachedInputMicrosPerMillion}} {
		total.Add(total, new(big.Int).Mul(big.NewInt(pair[0]), big.NewInt(pair[1])))
	}
	total.Add(total, big.NewInt(999999))
	total.Div(total, big.NewInt(1000000))
	if !total.IsInt64() {
		return nil, core.Invalid("pricing", "charge overflow")
	}
	return &core.Money{Currency: pricing.Currency, Micros: total.Int64()}, nil
}
func (c *client) Generate(ctx context.Context, req Request) (Stream, error) {
	if err := req.Execution.Validate(); err != nil {
		return nil, err
	}
	if err := req.ReservationID.Validate(); err != nil {
		return nil, err
	}
	if c.options.Operations == nil || c.options.Resolve == nil || c.options.CheckReservation == nil || c.options.CheckDestination == nil {
		return nil, core.NewError(core.FailedPrecondition, "model execution dependencies unavailable")
	}
	provider, err := c.options.Resolve(ctx, req.Execution.Principal, req.ModelID)
	if err != nil {
		return nil, err
	}
	if provider.Model.ID != req.ModelID {
		return nil, core.NewError(core.Forbidden, "model binding mismatch")
	}
	err = c.authorize(ctx, req.Execution.Principal, provider.Model.ResourceID, req.ModelID, policy.Execute, provider.Endpoint)
	if c.options.Observe != nil {
		c.options.Observe(ctx, "model.authorization", req.Execution.OperationID, map[string]any{"ModelID": req.ModelID, "ResourceID": provider.Model.ResourceID, "Action": "execute", "Code": core.CodeOf(err), "Reason": core.ReasonOf(err)})
	}
	if err != nil {
		return nil, err
	}
	if err = c.options.CheckReservation(ctx, req, provider); err != nil {
		return nil, err
	}
	if _, err = charge(provider.Pricing, Usage{}); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(provider.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, core.Invalid("endpoint", "invalid provider endpoint")
	}
	if err = c.options.CheckDestination(ctx, req.Execution, provider.Model.ResourceID, provider.Endpoint); err != nil {
		return nil, err
	}
	adapter, err := adapterFor(provider.Protocol)
	if err != nil {
		return nil, err
	}
	if c.options.PrepareInputs != nil {
		req.Inputs, err = c.options.PrepareInputs(ctx, req, provider)
		if err != nil {
			return nil, core.WithReason(err, core.ModelContextIncompatible)
		}
	}
	body, err := adapter.Encode(req, provider)
	if err != nil {
		return nil, err
	}
	if c.options.Observe != nil {
		c.options.Observe(ctx, "model.request", req.Execution.OperationID, map[string]any{"Model": provider.Model, "Request": sanitizedRequest(body)})
	}
	sum := sha256.Sum256(body)
	payload := hex.EncodeToString(sum[:])
	key := persistence.Key(req.Execution.Principal.OwnerID, req.Execution.OperationID)
	var attempt attemptRecord
	var receipt Receipt
	already := false
	var op operation.Operation
	err = c.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if req.LogicalID != "" {
			bindingKey := persistence.Key(req.Execution.Principal.OwnerID, req.LogicalID)
			binding := struct {
				Digest       string
				Source       StateSource
				CredentialID core.ID
				Pricing      *Pricing
			}{payload, provider.source(), provider.CredentialID, provider.Pricing}
			var old json.RawMessage
			encoded, _ := json.Marshal(binding)
			bindingErr := tx.Get(ctx, "agent.model_bindings", bindingKey, &old)
			if bindingErr == nil && !bytes.Equal(old, encoded) {
				return core.WithReason(core.NewError(core.FailedPrecondition, "model request binding changed"), core.ModelBindingChanged)
			}
			if bindingErr != nil && !persistence.Missing(bindingErr) {
				return bindingErr
			}
			if persistence.Missing(bindingErr) {
				if err := tx.Put(ctx, "agent.model_bindings", bindingKey, json.RawMessage(encoded)); err != nil {
					return err
				}
			}
		}
		e := tx.Get(ctx, "agent.model_attempts", key, &attempt)
		if e == nil {
			if attempt.Digest != payload || attempt.Source != provider.source() {
				return core.NewError(core.Conflict, "model operation payload changed")
			}
			already = true
			return nil
		}
		if !persistence.Missing(e) {
			return e
		}
		op, e = c.options.Operations.Begin(ctx, operation.BeginRequest{Execution: req.Execution, Mutation: core.Mutation{IdempotencyKey: string(req.Execution.OperationID)}, ResourceID: provider.Model.ResourceID, Action: "model.generate", InputDigest: payload})
		if e != nil {
			return e
		}
		if op.State != operation.Prepared {
			return core.NewError(core.FailedPrecondition, "model operation is already active or terminal")
		}
		version := op.Version
		op.State = operation.Running
		op.Version++
		if e = operation.NewRepository(c.store).Save(ctx, op, version); e != nil {
			return e
		}
		id := make([]byte, 16)
		if _, e = rand.Read(id); e != nil {
			return e
		}
		receipt = Receipt{ModelID: provider.Model.ID, ModelName: provider.Model.Name, Pricing: provider.Pricing, PricingInfo: provider.Model.PricingInfo, ID: core.ID(hex.EncodeToString(id)), OperationID: req.Execution.OperationID, CreatedAt: c.options.Now().UTC()}
		if e = c.receipts.Record(ctx, req.Execution.Principal.OwnerID, receipt); e != nil {
			return e
		}
		attempt = attemptRecord{Digest: payload, Source: provider.source(), ReceiptID: receipt.ID}
		return tx.Put(ctx, "agent.model_attempts", key, attempt)
	})
	if err != nil {
		return nil, err
	}
	if already {
		if attempt.Failure != nil {
			return nil, attempt.Failure
		}
		return nil, core.NewError(core.FailedPrecondition, "model attempt already dispatched; a new attempt is required")
	}
	completed := false
	defer func() {
		if !completed {
			recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = c.options.Operations.MarkUnknown(recordCtx, req.Execution, op.ID)
		}
	}()
	client := c.options.HTTPClient
	if c.options.ClientFor != nil {
		client, err = c.options.ClientFor(ctx, req, provider)
		if err != nil {
			return nil, err
		}
	}
	if client == nil {
		return nil, core.NewError(core.FailedPrecondition, "approved HTTP transport required")
	}
	transport := *client
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if transport.Timeout <= 0 {
		transport.Timeout = DefaultRequestTimeout
	}
	var parsed completion
	perform := func(secret core.Secret) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.Endpoint, bytes.NewReader(body))
		if err != nil {
			return core.Invalid("request", "cannot encode model request")
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", buildinfo.UserAgent())
		if !secret.Empty() {
			request.Header.Set("Authorization", "Bearer "+secret.Reveal())
		}
		started := c.options.Now()
		response, err := transport.Do(request)
		if err != nil {
			if core.CodeOf(err) == core.Forbidden {
				return core.WithReason(core.Wrap(core.Forbidden, "network access denied", err), core.NetworkAccessDenied)
			}
			return transportFailure(err)
		}
		defer response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 && c.options.Dispatched != nil {
			if err := c.options.Dispatched(ctx, req); err != nil {
				return err
			}
		}

		var data, decoded []byte
		var readErr error
		contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if response.StatusCode >= 200 && response.StatusCode < 300 && contentType == "text/event-stream" {
			data, decoded, receipt.Timing, readErr = readChatStream(response.Body, started, c.options.Now)
		} else {
			data, readErr = io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
			decoded = data
			receipt.Timing = &Timing{RequestMs: max(0, float64(c.options.Now().Sub(started))/float64(time.Millisecond))}
		}
		if c.options.ResponseLog != nil {
			raw := RawResponse{Status: response.StatusCode, ContentType: contentType, Body: string(data[:min(len(data), 8<<20)]), Incomplete: readErr != nil || len(data) > 8<<20}
			if err := c.options.ResponseLog(req, raw); err != nil {
				log.Printf("model response diagnostic log unavailable")
			}
		}
		if c.options.Observe != nil {
			c.options.Observe(ctx, "model.response", req.Execution.OperationID, map[string]any{"Status": response.StatusCode, "ContentType": contentType, "Incomplete": readErr != nil || len(data) > 8<<20})
		}
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = c.receipts.Record(recordCtx, req.Execution.Principal.OwnerID, receipt)
		cancel()
		if err != nil {
			return err
		}
		if len(data) > 8<<20 {
			return core.NewError(core.Unavailable, "invalid provider response")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return providerFailure(response.StatusCode, data, response.Header.Get("Retry-After"), c.options.Now())
		}
		if readErr != nil {
			return core.NewError(core.Unavailable, "provider response incomplete")
		}
		parsed, err = adapter.Decode(decoded, provider)
		return err
	}
	if provider.CredentialID != "" {
		if c.options.Broker == nil {
			return nil, core.NewError(core.FailedPrecondition, "credential broker required")
		}
		origin := endpoint.Scheme + "://" + endpoint.Host
		err = c.options.Broker.WithSecret(ctx, credential.Access{Execution: req.Execution, ResourceID: provider.Model.ResourceID, CredentialID: provider.CredentialID, Origin: origin}, perform)
	} else {
		err = perform(core.Secret{})
	}
	if err != nil {
		failure := FailureOf(err)
		if failure != nil {
			receipt.Failure, attempt.Failure = failure, failure
			receipt.OutcomeKnown = failure.OutcomeKnown
			if failure.NoCharge {
				receipt.Charged = &core.Money{Currency: provider.Pricing.Currency}
			}
			recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			persistErr := c.store.Write(recordCtx, func(ctx context.Context, tx persistence.Tx) error {
				if err := c.receipts.Record(ctx, req.Execution.Principal.OwnerID, receipt); err != nil {
					return err
				}
				if failure.OutcomeKnown {
					if _, err := c.options.Operations.Complete(ctx, req.Execution, operation.Completion{ID: op.ID, ExpectedVersion: op.Version, State: operation.Failed, ReceiptID: receipt.ID, FailureCode: failure.Code}); err != nil {
						return err
					}
				}
				return tx.Put(ctx, "agent.model_attempts", key, attempt)
			})
			if persistErr != nil {
				return nil, persistErr
			}
			completed = failure.OutcomeKnown
		}
		return nil, err
	}

	receipt.FinishReason = parsed.FinishReason
	receipt.ProviderRequestID = parsed.ID
	receipt.Usage = parsed.Usage
	for _, input := range req.Inputs {
		for _, part := range input.Media {
			if part.Kind == "audio" && len(part.Data) > 0 && !receipt.Usage.AudioUsageKnown {
				return nil, core.NewError(core.Unavailable, "provider omitted audio token usage; cost requires reconciliation")
			}
		}
	}
	receipt.Charged, err = charge(provider.Pricing, receipt.Usage)
	if err != nil {
		return nil, err
	}
	receipt.OutcomeKnown = true
	// Persist known billing evidence even when the following tool-output parsing
	// discovers an unusable completion or the caller stops consuming.
	evidenceCtx, evidenceCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer evidenceCancel()
	if err = c.receipts.Record(evidenceCtx, req.Execution.Principal.OwnerID, receipt); err != nil {
		return nil, err
	}
	// Settlement consumes trusted receipt evidence, not permission for a new call.
	// It must run before a live-permission completion check that can reject a
	// canceled execution; receipts and known spend survive that rejection.
	if c.options.Settle != nil {
		if err = c.options.Settle(evidenceCtx, req.Execution, receipt); err != nil {
			return nil, err
		}
	}
	// Validate execution payload only after retaining known billing evidence.
	if len(parsed.Calls) > 64 || len(parsed.Text) > 1<<20 {
		return nil, core.NewError(core.Unavailable, "provider completion exceeds execution limits")
	}
	deltas := []Delta{}
	if parsed.State != nil {
		if len(parsed.State.Payload)+len(parsed.Text) > 1<<20 {
			return nil, core.NewError(core.Unavailable, "provider completion exceeds execution limits")
		}
		deltas = append(deltas, Delta{Kind: ProviderStateDelta, ProviderState: parsed.State})
	}
	if parsed.Text != "" {
		deltas = append(deltas, Delta{Kind: TextDelta, Text: parsed.Text})
	}
	for _, call := range parsed.Calls {
		if !functionName.MatchString(call.Name) || call.ID == "" || !json.Valid(call.Arguments) {
			return nil, core.NewError(core.Unavailable, "provider returned invalid tool call")
		}
		deltas = append(deltas, Delta{Kind: ToolCallDelta, ToolCall: &call})
	}
	deltas = append(deltas, Delta{Kind: Completed, Receipt: &receipt})
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = c.store.Write(persistCtx, func(ctx context.Context, tx persistence.Tx) error {
		if err := c.receipts.Record(ctx, req.Execution.Principal.OwnerID, receipt); err != nil {
			return err
		}
		if _, err := c.options.Operations.Complete(ctx, req.Execution, operation.Completion{ID: op.ID, ExpectedVersion: op.Version, State: operation.Succeeded, ReceiptID: receipt.ID, SafeResult: json.RawMessage(`{"completed":true}`)}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.model_attempts", key, attempt)
	})
	if err != nil {
		return nil, err
	}
	completed = true

	return &stream{deltas: deltas}, nil
}

type stream struct {
	mu     sync.Mutex
	deltas []Delta
	index  int
	closed bool
}

func (s *stream) Next(ctx context.Context) (Delta, error) {
	if err := persistence.CheckContext(ctx); err != nil {
		return Delta{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Delta{}, core.NewError(core.Canceled, "stream closed")
	}
	if s.index >= len(s.deltas) {
		return Delta{}, io.EOF
	}
	d := s.deltas[s.index]
	s.index++
	return d, nil
}
func (s *stream) Close() error { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return nil }
