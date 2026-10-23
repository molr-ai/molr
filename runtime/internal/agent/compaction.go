package agent

import (
	"context"
	"strings"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// ContextStatus distinguishes provider measurements from conservative estimates.
type ContextStatus struct {
	ModelID                    core.ID
	Limit                      int
	Tokens                     int64
	Source                     string
	CompressionThresholdTokens int
	Compressions               int
}

// A local result shared only between accepting a response and detecting compression.
// Inputs, definitions and usage do not change across that checkpoint boundary.
type contextEstimate struct {
	tokens      int64
	source      string
	modelSource model.StateSource
}

type contextUsage struct {
	ModelID core.ID
	Source  model.StateSource
	// A new summary needs one ordinary model measurement before auto-compacting again.
	AwaitingMeasurement bool
	Tokens              int64
	// Estimates by content hash permit changed automatic sources without hiding
	// newly appended messages behind a decrease elsewhere in the request.
	Inputs map[string]int64
	Tools  string
}
type contextArchive struct {
	AssistantID core.ID
	EventID     core.ID
	Inputs      []model.Input
	Automatic   *automaticContext
}

func textTokens(s string) int64 {
	var ascii, other int64
	for _, r := range s {
		if r < 128 {
			ascii++
		} else {
			other += 2
		}
	}
	return (ascii+2)/3 + other
}
func inputEstimate(in model.Input, source model.StateSource) int64 {
	n := textTokens(in.Text) + 16
	for _, c := range in.Calls {
		n += textTokens(c.Name) + textTokens(string(c.Arguments)) + 16
	}
	if reasoning, _ := model.ReplayedReasoning(in, source); reasoning != nil {
		n += textTokens(*reasoning)
	}
	for _, part := range in.Media {
		n += int64(len(part.Data)) + 4096
	}
	return n
}
func definitionsEstimate(defs []model.ToolDefinition) int64 {
	var n int64
	for _, d := range defs {
		n += textTokens(d.Name) + textTokens(d.Description) + textTokens(string(d.InputSchema)) + 16
	}
	return n
}
func usageInputs(inputs []model.Input, source model.StateSource) map[string]int64 {
	result := map[string]int64{}
	for _, in := range modelInputs(inputs) {
		n := inputEstimate(in, source)
		// Hash only state the adapter replays; private reasoning must not look like a new input.
		reasoning, _ := model.ReplayedReasoning(in, source)
		in.ProviderState = nil
		result[contextHash(struct {
			Input     model.Input
			Reasoning *string
		}{in, reasoning})] += n
	}
	return result
}
func contextTokens(p turnProgress, id core.ID, source model.StateSource) (int64, string) {
	tools := ""
	if p.Usage != nil && p.Usage.ModelID == id && p.Usage.Source == source {
		tools = contextHash(p.Definitions)
	}
	return contextTokensFromInputs(p, id, source, usageInputs(p.Inputs, source), tools)
}

func contextTokensFromInputs(p turnProgress, id core.ID, source model.StateSource, current map[string]int64, tools string) (int64, string) {
	if p.Usage != nil && p.Usage.ModelID == id && p.Usage.Source == source {
		tokens := p.Usage.Tokens
		changed := false
		for hash, n := range current {
			if d := n - p.Usage.Inputs[hash]; d > 0 {
				tokens += d
				changed = true
			}
		}
		if p.Usage.Tools != tools {
			tokens += definitionsEstimate(p.Definitions)
			changed = true
		}
		if p.Usage.AwaitingMeasurement {
			return tokens, "estimated"
		}
		if changed {
			return tokens, "provider_with_estimate"
		}
		return tokens, "provider"
	}
	tokens := definitionsEstimate(p.Definitions)
	for _, n := range current {
		tokens += n
	}
	return tokens, "estimated"
}
func (e *Engine) measureContext(ctx context.Context, t Turn, id core.ID, p turnProgress) *contextUsage {
	idOp, err := e.modelAttemptID(ctx, t.Principal.OwnerID, modelOperationID(t, p.Step))
	if err != nil {
		return nil
	}
	receipt, err := model.NewReceipts(e.store).ByOperation(ctx, t.Principal.OwnerID, idOp)
	if err != nil || !receipt.OutcomeKnown || receipt.Usage.InputTokens <= 0 {
		return nil
	}
	source, err := e.modelSource(ctx, t, id)
	if err != nil {
		return nil
	}
	// Provider output usage includes reasoning that may never be replayed. Keep
	// the measured input baseline; the actual response is an estimated increment.
	return &contextUsage{ModelID: id, Source: source, Tokens: receipt.Usage.InputTokens, Inputs: usageInputs(p.Inputs, source), Tools: contextHash(p.Definitions)}
}
func (e *Engine) modelSource(ctx context.Context, t Turn, id core.ID) (model.StateSource, error) {
	if e.options.ModelSource != nil {
		return e.options.ModelSource(ctx, t.Principal, id)
	}
	return model.StateSource{}, nil
}

func (e *Engine) contextWindow(ctx context.Context, t Turn, id core.ID) (int, error) {
	if e.options.ContextWindow != nil {
		return e.options.ContextWindow(ctx, t.Principal, id)
	}
	return model.DefaultContextLimit, nil
}

// Read the assistant preference at each safe boundary so changes apply to an
// ongoing loop. The active model window always bounds the preference.
func (e *Engine) contextLimits(ctx context.Context, t Turn, id core.ID) (int, int, error) {
	window, err := e.contextWindow(ctx, t, id)
	if err != nil {
		return 0, 0, err
	}
	configured := 0
	if e.options.CompressionTokens != nil {
		configured, err = e.options.CompressionTokens(ctx, t.Principal, t.Event.AssistantID)
		if err != nil {
			return 0, 0, err
		}
	}
	return window, core.CompressionTokens(window, configured), nil
}

func (e *Engine) recordContextStatus(ctx context.Context, t Turn, value ContextStatus) error {
	return e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		if status.Context != nil {
			value.Compressions += status.Context.Compressions
		}
		status.Context = &value
		return e.saveStatus(ctx, tx, key, status)
	})
}

var compactionInstruction = prompt.Text("compaction", nil)

func compactionError(reason core.Reason) error {
	return core.WithReason(core.NewError(core.FailedPrecondition, "context compaction did not commit; original history retained"), reason)
}

func (e *Engine) compactContext(ctx context.Context, t Turn, id core.ID, p *turnProgress, force bool) (interrupted bool, resultErr error) {
	limit, threshold, err := e.contextLimits(ctx, t, id)
	if err != nil {
		return false, err
	}
	cap, err := e.outputLimit(ctx, t, id)
	if err != nil {
		return false, err
	}
	// Cap summary output independently of advisory input estimates.
	// Respect explicit/provider output caps; small windows never inherit a 32K
	// output reservation that consumes their entire context.
	output := min(cap, limit/4)
	modelSource, err := e.modelSource(ctx, t, id)
	if err != nil {
		return false, err
	}
	tokens, source := contextTokens(*p, id, modelSource)
	status := ContextStatus{ModelID: id, Limit: limit, Tokens: tokens, Source: source, CompressionThresholdTokens: threshold}
	e.options.Trace.Record(ctx, "context.usage", "succeeded", "", status)
	if err = e.recordContextStatus(ctx, t, status); err != nil {
		return false, err
	}
	if !force && tokens < int64(threshold) {
		return false, nil
	}

	archive := contextArchive{AssistantID: t.Event.AssistantID, EventID: t.Event.ID, Inputs: p.Inputs, Automatic: p.Automatic}
	archiveID := stableID(string(t.Event.AssistantID), "context-archive", contextHash(archive))

	e.options.Trace.Record(ctx, "context.compaction", "started", archiveID, map[string]any{"Tokens": tokens, "Limit": limit, "ArchiveID": archiveID, "Strategy": "full_context_append", "MaxOutputTokens": output})
	defer func() {
		if resultErr != nil || interrupted {
			state := "failed"
			reason := string(core.ReasonOf(resultErr))
			if reason == "" {
				reason = string(core.CodeOf(resultErr))
			}
			if interrupted {
				state, reason = "canceled", "expedited"
			}
			e.options.Trace.Record(ctx, "context.compaction", state, archiveID, map[string]any{"ArchiveID": archiveID, "Reason": reason, "OriginalHistoryRetained": true})
		}
	}()
	history := contextHistory(p.Inputs)
	// Preserve the complete request prefix, including automatic context and
	// provider state, so the provider can reuse its KV cache.
	summaryInputs := append([]model.Input{}, p.Inputs...)
	summaryInputs = append(summaryInputs, model.Input{Role: model.User, Text: compactionInstruction})
	request := turnProgress{Inputs: summaryInputs, Definitions: p.Definitions, Joined: p.Joined, Usage: p.Usage}
	// Include model, tools and output budget in identity; changing configuration
	// must not replay a truncated summary generated under the previous budget.
	op := stableID(string(archiveID), "full-context-summary-v1", string(id), contextHash(summaryInputs), contextHash(p.Definitions), contextHash(output))
	result, interrupted, err := e.generateContextStep(ctx, t, id, p.Definitions, request, op, output)
	if err != nil || interrupted {
		return interrupted, err
	}
	attemptID, err := e.modelAttemptID(ctx, t.Principal.OwnerID, op)
	if err != nil {
		return false, err
	}
	receipt, err := model.NewReceipts(e.store).ByOperation(ctx, t.Principal.OwnerID, attemptID)
	if err != nil {
		return false, err
	}
	if receipt.FinishReason == "length" {
		return false, compactionError(core.ContextSummaryTruncated)
	}
	if receipt.FinishReason != "stop" || len(result.Calls) > 0 || strings.TrimSpace(result.Text) == "" {
		return false, compactionError(core.ContextSummaryInvalid)
	}
	var readIDs []core.ID
	for _, in := range history[1:] {
		readIDs = append(readIDs, in.ReadMessageIDs...)
	}
	// This is a synthetic historical checkpoint, not an assistant completion.
	// Keep summarizer reasoning only in its receipt/trace, not the active context.
	next := []model.Input{history[0], {Role: model.User, Text: prompt.Text("summary", result.Text), ReadMessageIDs: readIDs}}
	for i := len(history) - 1; i > 0; i-- {
		if strings.HasPrefix(history[i].Text, "Runtime event:") {
			next = append(next, history[i])
			break
		}
	}
	if p.Automatic != nil {
		next = append(next, contextInput(p.Automatic))
	}
	candidate := *p
	candidate.Inputs, candidate.Usage = next, nil
	current, toolHash := usageInputs(next, modelSource), contextHash(p.Definitions)
	after, _ := contextTokensFromInputs(candidate, id, modelSource, current, toolHash)
	candidate.Usage = &contextUsage{ModelID: id, Source: modelSource, Tokens: after, Inputs: current, Tools: toolHash, AwaitingMeasurement: true}
	// Checkpoint, progress reference, status and save trace commit together.
	status.Tokens, status.Source, status.Compressions = after, "estimated", 1
	if err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := e.saveProgress(checkpointCause(ctx, "context_compaction"), t, &candidate, progressIfChanged); err != nil {
			return err
		}
		if err := e.recordContextStatus(ctx, t, status); err != nil {
			return err
		}
		if source, ok := ctx.Value(compressionContextKey{}).(event.AssistantEvent); ok {
			if err := e.finishCompression(ctx, t, source, "succeeded", ""); err != nil {
				return err
			}
		}
		e.options.Trace.Record(ctx, "context.compaction", "succeeded", archiveID, map[string]any{"ArchiveID": archiveID, "BeforeTokens": tokens, "AfterTokens": after, "Strategy": "full_context_append", "ModelCalls": 1, "SummarizedMessages": len(history) - 1, "RetainedMessages": 0, "MaxOutputTokens": output})
		return nil
	}); err != nil {
		return false, err
	}
	*p = candidate
	return false, nil
}
