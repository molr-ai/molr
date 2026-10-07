package agent

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// ContextStatus distinguishes provider measurements from conservative estimates.
type ContextStatus struct {
	ModelID              core.ID
	Limit                int
	Tokens               int64
	Source               string
	CompressionThreshold float64
	Compressions         int
}
type contextUsage struct {
	ModelID core.ID
	Tokens  int64
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
func inputEstimate(in model.Input) int64 {
	// Estimate text conservatively (ASCII/3, two tokens per non-ASCII rune).
	// This is explicitly labeled, never presented as a provider measurement.
	n := textTokens(in.Text) + 16
	for _, c := range in.Calls {
		n += textTokens(c.Name) + textTokens(string(c.Arguments)) + 16
	}
	if in.ProviderState != nil {
		// Opaque state also contains the text and calls already priced above.
		// Count only extension fields here (not a second copy of the message).
		var state map[string]json.RawMessage
		if json.Unmarshal(in.ProviderState.Payload, &state) == nil && state != nil {
			for key, value := range state {
				if key == "role" || key == "content" || key == "tool_calls" {
					continue
				}
				var text string
				if json.Unmarshal(value, &text) == nil {
					n += textTokens(text)
				} else {
					n += textTokens(string(value))
				}
			}
		} else {
			n += textTokens(string(in.ProviderState.Payload))
		}
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
func usageInputs(inputs []model.Input) map[string]int64 {
	result := map[string]int64{}
	for _, in := range modelInputs(inputs) {
		result[contextHash(in)] += inputEstimate(in)
	}
	return result
}
func contextTokens(p turnProgress, id core.ID) (int64, string) {
	current := usageInputs(p.Inputs)
	if p.Usage != nil && p.Usage.ModelID == id {
		tokens := p.Usage.Tokens
		changed := false
		for hash, n := range current {
			if d := n - p.Usage.Inputs[hash]; d > 0 {
				tokens += d
				changed = true
			}
		}
		if p.Usage.Tools != contextHash(p.Definitions) {
			tokens += definitionsEstimate(p.Definitions)
			changed = true
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
func (e *Engine) measureContext(ctx context.Context, t Turn, id core.ID, p turnProgress, response model.Input) *contextUsage {
	idOp, err := e.modelAttemptID(ctx, t.Principal.OwnerID, modelOperationID(t, p.Step))
	if err != nil {
		return nil
	}
	receipt, err := model.NewReceipts(e.store).ByOperation(ctx, t.Principal.OwnerID, idOp)
	if err != nil || !receipt.OutcomeKnown || receipt.Usage.InputTokens <= 0 {
		return nil
	}
	inputs := append(append([]model.Input{}, p.Inputs...), response)
	return &contextUsage{ModelID: id, Tokens: receipt.Usage.InputTokens + receipt.Usage.OutputTokens, Inputs: usageInputs(inputs), Tools: contextHash(p.Definitions)}
}
func (e *Engine) contextWindow(ctx context.Context, t Turn, id core.ID) (int, error) {
	if e.options.ContextWindow != nil {
		return e.options.ContextWindow(ctx, t.Principal, id)
	}
	return model.DefaultContextLimit, nil
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

// Choose a complete, tool-balanced prefix and keep a token-budgeted recent tail.
// No JSON slicing and no repeated summarization of incrementally fed chunks.
func compactionCut(history []model.Input, retain int64) (int, error) {
	history = modelInputs(history)
	if len(history) < 2 {
		return 0, contextCapacityError()
	}
	remaining := int64(0)
	for _, in := range history {
		remaining += inputEstimate(in)
	}
	pending := map[string]bool{}
	cut := 0
	for i, in := range history {
		if in.Role == model.Tool {
			if !pending[in.ReplyTo] {
				return 0, compactionError(core.ContextSummaryInvalid)
			}
			delete(pending, in.ReplyTo)
		}
		for _, call := range in.Calls {
			if pending[call.ID] {
				return 0, compactionError(core.ContextSummaryInvalid)
			}
			pending[call.ID] = true
		}
		remaining -= inputEstimate(in)
		if i > 0 && len(pending) == 0 && remaining <= retain && cut == 0 {
			cut = i + 1
		}
	}
	if len(pending) != 0 {
		return 0, compactionError(core.ContextSummaryInvalid)
	}
	if cut <= 1 {
		return 0, contextCapacityError()
	}
	return cut, nil
}

func compactionError(reason core.Reason) error {
	return core.WithReason(core.NewError(core.FailedPrecondition, "context compaction did not commit; original history retained"), reason)
}

func (e *Engine) compactContext(ctx context.Context, t Turn, id core.ID, p *turnProgress, force bool) (interrupted bool, resultErr error) {
	limit, err := e.contextWindow(ctx, t, id)
	if err != nil {
		return false, err
	}
	cap, err := e.outputLimit(ctx, t, id)
	if err != nil {
		return false, err
	}
	// Reserve space for both reasoning and summary output, plus the instruction.
	// Respect explicit/provider output caps; small windows never inherit a 32K
	// output reservation that consumes their entire context.
	output := min(cap, limit/4)
	margin := min(1024, limit/32)
	threshold := int64(math.Ceil(float64(limit) * e.options.CompressionThreshold))
	tokens, source := contextTokens(*p, id)
	status := ContextStatus{ModelID: id, Limit: limit, Tokens: tokens, Source: source, CompressionThreshold: e.options.CompressionThreshold}
	e.options.Trace.Record(ctx, "context.usage", "succeeded", "", status)
	if err = e.recordContextStatus(ctx, t, status); err != nil {
		return false, err
	}
	if !force && tokens < threshold {
		return false, nil
	}

	archive := contextArchive{AssistantID: t.Event.AssistantID, EventID: t.Event.ID, Inputs: p.Inputs, Automatic: p.Automatic}
	archiveID := stableID(string(t.Event.AssistantID), "context-archive", contextHash(archive))
	if err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.context_archives", persistence.Key(t.Principal.OwnerID, archiveID), archive)
	}); err != nil {
		return false, err
	}
	e.options.Trace.Record(ctx, "context.compaction", "started", archiveID, map[string]any{"Tokens": tokens, "Limit": limit, "ArchiveID": archiveID, "Strategy": "single_request", "MaxOutputTokens": output})
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
	var historyTokens int64
	for _, in := range modelInputs(history) {
		historyTokens += inputEstimate(in)
	}
	cut, err := compactionCut(history, min(int64(limit)/3, historyTokens/3))
	if err != nil {
		return false, err
	}
	summaryInputs := append([]model.Input{}, history[:cut]...)
	summaryInputs = append(summaryInputs, model.Input{Role: model.User, Text: compactionInstruction})
	request := turnProgress{Inputs: summaryInputs, Definitions: p.Definitions, Joined: p.Joined}
	inputTokens, _ := contextTokens(request, id)
	if output < 1 || inputTokens+int64(output+margin) > int64(limit) {
		return false, compactionError(core.ContextSummaryInputTooLarge)
	}
	// Include model, tools and output budget in identity; changing configuration
	// must not replay a truncated summary generated under the previous budget.
	op := stableID(string(archiveID), "single-summary-v2", string(id), contextHash(summaryInputs), contextHash(p.Definitions), contextHash(output))
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
	for _, in := range history[1:cut] {
		readIDs = append(readIDs, in.ReadMessageIDs...)
	}
	// This is a synthetic historical checkpoint, not an assistant completion.
	// Keep summarizer reasoning only in its receipt/trace, not the active context.
	next := []model.Input{history[0], {Role: model.User, Text: prompt.Text("summary", result.Text), ReadMessageIDs: readIDs}}
	for i := len(history) - 1; i > 0; i-- {
		if strings.HasPrefix(history[i].Text, "Runtime event:") {
			if i < cut {
				next = append(next, history[i])
			}
			break
		}
	}
	next = append(next, history[cut:]...)
	if p.Automatic != nil {
		next = append(next, contextInput(p.Automatic))
	}
	candidate := *p
	candidate.Inputs, candidate.Usage = next, nil
	after, _ := contextTokens(candidate, id)
	beforeEstimate, _ := contextTokens(turnProgress{Inputs: p.Inputs, Definitions: p.Definitions}, id)
	if after >= beforeEstimate || after >= threshold {
		return false, compactionError(core.ContextCompactionIneffective)
	}
	// Checkpoint, progress reference, status and save trace commit together.
	status.Tokens, status.Source, status.Compressions = after, "estimated", 1
	if err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := e.saveProgress(checkpointCause(ctx, "context_compaction"), t, &candidate); err != nil {
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
		e.options.Trace.Record(ctx, "context.compaction", "succeeded", archiveID, map[string]any{"ArchiveID": archiveID, "BeforeTokens": tokens, "AfterTokens": after, "Strategy": "single_request", "ModelCalls": 1, "SummarizedMessages": cut - 1, "RetainedMessages": len(history) - cut, "MaxOutputTokens": output})
		return nil
	}); err != nil {
		return false, err
	}
	*p = candidate
	return false, nil
}
func contextCapacityError() error {
	return core.WithReason(core.NewError(core.FailedPrecondition, "context cannot fit after compaction; original history retained"), core.ContextLimit)
}
