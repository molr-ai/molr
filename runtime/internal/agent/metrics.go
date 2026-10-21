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
	ReferenceCalls  int
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
			ops, err := persistence.Select[operation.Operation](ctx, e.store, "agent.operations", persistence.Query{
				Where:  []persistence.Filter{persistence.Eq("key", persistence.Key(p.OwnerID, message.Source.OperationID))},
				Fields: []string{"AssistantID", "ExecutionID"}, Unordered: true, Limit: 1,
			})
			if err != nil {
				return err
			}
			if len(ops) > 0 && ops[0].AssistantID == chat.PeerID {
				out.TurnID = ops[0].ExecutionID
			}
		}
		if out.TurnID == "" {
			return nil
		}
		jobs, err := persistence.Select[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Query{
			Where:     []persistence.Filter{persistence.Eq("Event.OwnerID", p.OwnerID), persistence.Eq("Event.AssistantID", chat.PeerID), persistence.Eq("ExecutionID", out.TurnID)},
			Unordered: true, Limit: 1, Fields: []string{"State", "Attempts"},
		})
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		job := jobs[0]
		out.State = string(job.State)
		out.Retries = max(0, job.Attempts-1)
		terminal := job.State == scheduler.Interrupted || job.State == scheduler.Done || job.State == scheduler.Canceled || job.State == scheduler.Failed
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
		operations, err := persistence.Select[operation.Operation](ctx, e.store, "agent.operations", persistence.Query{
			Where:     []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", chat.PeerID), persistence.Eq("ExecutionID", out.TurnID)},
			Unordered: true, Fields: []string{"ID", "Action", "State", "CreatedAt"},
		})
		if err != nil {
			return err
		}
		receipts := model.NewReceipts(e.store)
		usage := model.Usage{AudioUsageKnown: true}
		out.UsageComplete, out.CostComplete = true, true
		charges := map[string]int64{}
		models := map[string]bool{}
		var first, seconds float64
		var timedOutput int64
		operationsSettled := true
		for _, op := range operations {
			if op.State == operation.Running || op.State == operation.Prepared || op.State == operation.Unknown {
				operationsSettled = false
			}
			if op.Action != "model.generate" {
				out.ToolCalls++
				continue
			}
			out.ModelCalls++
			call := ModelCallMetrics{OperationID: op.ID, State: op.State, CreatedAt: op.CreatedAt}
			receipt, err := receipts.ByOperation(ctx, p.OwnerID, op.ID)
			if err != nil && !persistence.Missing(err) {
				return err
			}
			found := err == nil
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
				if receipt.PricingInfo != nil && receipt.PricingInfo.Basis == "subscription-reference" {
					out.ReferenceCalls++
				}
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
