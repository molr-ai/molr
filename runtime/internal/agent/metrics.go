package agent

import (
	"context"
	"sort"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// Turn timing is raw lifecycle evidence; aggregate metrics are never persisted.
type turnTiming struct {
	StartedAt time.Time
	EndedAt   *time.Time
}

func (e *Engine) startTurnTiming(ctx context.Context, t Turn) error {
	return e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(t.Principal.OwnerID, t.Principal.ExecutionID)
		var timing turnTiming
		if err := tx.Get(ctx, "agent.turn_timing", key, &timing); err != nil && !persistence.Missing(err) {
			return err
		}
		if timing.StartedAt.IsZero() {
			timing.StartedAt = e.options.Now().UTC()
		}
		timing.EndedAt = nil
		return tx.Put(ctx, "agent.turn_timing", key, timing)
	})
}
func (e *Engine) finishTurnTiming(ctx context.Context, t Turn) error {
	return e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(t.Principal.OwnerID, t.Principal.ExecutionID)
		var timing turnTiming
		if err := tx.Get(ctx, "agent.turn_timing", key, &timing); err != nil {
			return err
		}
		now := e.options.Now().UTC()
		timing.EndedAt = &now
		return tx.Put(ctx, "agent.turn_timing", key, timing)
	})
}

type ModelCallMetrics struct {
	OperationID core.ID
	ModelName   string
	State       operation.State
	CreatedAt   time.Time
	Usage       *model.Usage
	Charged     *core.Money
	Timing      *model.Timing
}
type TurnMetrics struct {
	TurnID          core.ID
	State           string
	Finalized       bool
	UpdatedAt       time.Time
	StartedAt       *time.Time
	EndedAt         *time.Time
	ElapsedMs       *float64
	Usage           *model.Usage
	Charges         []core.Money
	UsageComplete   bool
	CostComplete    bool
	FirstTokenMs    *float64
	TokensPerSecond *float64
	TimingSamples   int
	SpeedSamples    int
	ModelCalls      int
	CompletedCalls  int
	ToolCalls       int
	Retries         int
	Models          []string
	Calls           []ModelCallMetrics
}

// MessageMetrics checks the conversation boundary before reading private execution
// evidence. All reads share one snapshot, including lifecycle and billing data.
func (e *Engine) MessageMetrics(ctx context.Context, p core.Principal, conversationID, messageID core.ID) (out TurnMetrics, err error) {
	if err = p.RequireOwner(); err != nil {
		return
	}
	for _, id := range []core.ID{conversationID, messageID} {
		if err = id.Validate(); err != nil {
			return
		}
	}
	if e.options.Dependencies.Policy == nil {
		return out, core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	if err = e.options.Dependencies.Policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "conversation", ID: conversationID}, Action: policy.Read}); err != nil {
		return
	}
	err = e.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		chat, err := conversation.NewRepository(e.store).Get(ctx, p.OwnerID, conversationID)
		if err != nil {
			return err
		}
		var message conversation.Message
		if err = tx.Get(ctx, "im.messages", persistence.Key(p.OwnerID, messageID), &message); err != nil {
			return err
		}
		if message.ConversationID != conversationID || message.Sender != conversation.Assistant {
			return core.NewError(core.NotFound, "assistant message not found")
		}
		out = TurnMetrics{State: "unavailable", UpdatedAt: e.options.Now().UTC(), Charges: []core.Money{}, Models: []string{}, Calls: []ModelCallMetrics{}}
		out.TurnID = message.Source.ExecutionID
		// Older messages already carry the trusted publication operation identity.
		if out.TurnID == "" && message.Source.OperationID != "" {
			op, err := operation.NewRepository(e.store).Get(ctx, p.OwnerID, message.Source.OperationID)
			if err != nil && !persistence.Missing(err) {
				return err
			}
			if err == nil && op.AssistantID == chat.PeerID {
				out.TurnID = op.ExecutionID
			}
		}
		if out.TurnID == "" {
			return nil
		}
		jobs, err := persistence.Scan[scheduler.Job](ctx, e.store, "agent.jobs")
		if err != nil {
			return err
		}
		var job *scheduler.Job
		for i := range jobs {
			candidate := &jobs[i]
			if candidate.Event.OwnerID == p.OwnerID && candidate.Event.AssistantID == chat.PeerID && candidate.ExecutionID == out.TurnID {
				job = candidate
				break
			}
		}
		if job == nil {
			return nil
		}
		out.State = string(job.State)
		out.Retries = max(0, job.Attempts-1)
		terminal := job.State == scheduler.Done || job.State == scheduler.Canceled || job.State == scheduler.Failed
		var timing turnTiming
		err = tx.Get(ctx, "agent.turn_timing", persistence.Key(p.OwnerID, out.TurnID), &timing)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		timingSettled := persistence.Missing(err) // historical timing cannot be reconstructed
		if err == nil {
			out.StartedAt = &timing.StartedAt
			end := out.UpdatedAt
			if terminal && timing.EndedAt != nil {
				out.EndedAt = timing.EndedAt
				end = *timing.EndedAt
			}
			elapsed := max(0, float64(end.Sub(timing.StartedAt))/float64(time.Millisecond))
			out.ElapsedMs = &elapsed
			timingSettled = timing.EndedAt != nil
		}
		operations, err := persistence.Scan[operation.Operation](ctx, e.store, "agent.operations")
		if err != nil {
			return err
		}
		// One scan avoids an O(calls * all receipts) lookup on each click.
		records, err := tx.Scan(ctx, "agent.model_receipts")
		if err != nil {
			return err
		}
		receipts := map[core.ID]model.Receipt{}
		for _, record := range records {
			if len(record.Key) <= len(p.OwnerID) || record.Key[:len(p.OwnerID)+1] != string(p.OwnerID)+"/" {
				continue
			}
			receipt, err := persistence.Decode[model.Receipt](record)
			if err != nil {
				return err
			}
			receipts[receipt.OperationID] = receipt
		}
		usage := model.Usage{AudioUsageKnown: true}
		out.UsageComplete, out.CostComplete = true, true
		charges := map[string]int64{}
		models := map[string]bool{}
		var first, seconds float64
		var timedOutput int64
		operationsSettled := true
		for _, op := range operations {
			if op.OwnerID != p.OwnerID || op.AssistantID != chat.PeerID || op.ExecutionID != out.TurnID {
				continue
			}
			if op.State == operation.Running || op.State == operation.Prepared || op.State == operation.Unknown {
				operationsSettled = false
			}
			if op.Action != "model.generate" {
				out.ToolCalls++
				continue
			}
			out.ModelCalls++
			call := ModelCallMetrics{OperationID: op.ID, State: op.State, CreatedAt: op.CreatedAt}
			receipt, found := receipts[op.ID]
			if found {
				call.ModelName, call.Timing = receipt.ModelName, receipt.Timing
				if receipt.ModelName != "" {
					models[receipt.ModelName] = true
				}
			}
			if found && receipt.OutcomeKnown {
				out.CompletedCalls++
				call.Usage = &receipt.Usage
				usage.AudioInputTokens += receipt.Usage.AudioInputTokens
				usage.AudioUsageKnown = usage.AudioUsageKnown && receipt.Usage.AudioUsageKnown
				usage.InputTokens += receipt.Usage.InputTokens
				usage.CachedTokens += receipt.Usage.CachedTokens
				usage.OutputTokens += receipt.Usage.OutputTokens
				call.Charged = receipt.Charged
				if receipt.Charged != nil {
					charges[receipt.Charged.Currency] += receipt.Charged.Micros
				} else {
					out.CostComplete = false
				}
				if receipt.Timing != nil {
					if v := receipt.Timing.FirstTokenMs; v != nil {
						first += *v
						out.TimingSamples++
					}
					if v := receipt.Timing.GenerationMs; v != nil && *v > 0 {
						seconds += *v / 1000
						timedOutput += receipt.Usage.OutputTokens
						out.SpeedSamples++
					}
				}
			} else {
				out.UsageComplete = false
				out.CostComplete = false
			}
			out.Calls = append(out.Calls, call)
		}
		if out.CompletedCalls > 0 || out.ModelCalls == 0 {
			out.Usage = &usage
		}
		for currency, micros := range charges {
			out.Charges = append(out.Charges, core.Money{Currency: currency, Micros: micros})
		}
		for name := range models {
			out.Models = append(out.Models, name)
		}
		sort.Strings(out.Models)
		sort.Slice(out.Charges, func(i, j int) bool { return out.Charges[i].Currency < out.Charges[j].Currency })
		sort.Slice(out.Calls, func(i, j int) bool {
			if out.Calls[i].CreatedAt.Equal(out.Calls[j].CreatedAt) {
				return out.Calls[i].OperationID < out.Calls[j].OperationID
			}
			return out.Calls[i].CreatedAt.Before(out.Calls[j].CreatedAt)
		})
		if out.TimingSamples > 0 {
			v := first / float64(out.TimingSamples)
			out.FirstTokenMs = &v
		}
		if seconds > 0 {
			v := float64(timedOutput) / seconds
			out.TokensPerSecond = &v
		}
		out.Finalized = terminal && timingSettled && operationsSettled && out.UsageComplete && out.CostComplete
		return nil
	})
	return
}
