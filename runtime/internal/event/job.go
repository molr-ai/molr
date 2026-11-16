package event

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"time"
)

type JobState string

const (
	Queued      JobState = "queued"
	Running     JobState = "running"
	Done        JobState = "done"
	Retry       JobState = "retry"
	Failed      JobState = "failed"
	NeedsReview JobState = "needs_review"
	Canceled    JobState = "canceled"
	Interrupted JobState = "interrupted"
)

type Job struct {
	InterruptedAt *time.Time `json:",omitempty"`
	// RecoveryEventID fences an interrupted job without reporting user cancellation.
	RecoveryEventID core.ID `json:",omitempty"`
	// QueueKey orders independent work before dreams, then exact UTC time and ID.
	QueueKey string
	// JoinedTo binds expedited input to its existing turn until checkpoint commit.
	JoinedTo    core.ID `json:",omitempty"`
	Event       AssistantEvent
	State       JobState
	ExecutionID core.ID
	Attempts    int
	NotBefore   time.Time
	FailureCode core.Code
	Version     core.Version
}

// DispatchSignal is a coalescible hint; durable jobs remain the source of truth.
func DispatchSignal() persistence.SignalKey {
	return persistence.SignalKey{Namespace: "agent.dispatch", Key: "ready"}
}

// AssistantInputSignal wakes in-flight model requests for one assistant. The
// durable inbox decides whether a committed change can interrupt that request.
func AssistantInputSignal(owner, assistant core.ID) persistence.SignalKey {
	return persistence.SignalKey{Namespace: "agent.input", Key: persistence.Key(owner, assistant)}
}

// SaveJob keeps the indexed queue order and commit notification together. All
// job transitions, including joined inputs and recovery, use this boundary.
func SaveJob(ctx context.Context, tx persistence.Tx, job Job) error {
	priority := "0/"
	if job.Event.Kind == DreamDue {
		priority = "1/"
	}
	job.QueueKey = priority + job.NotBefore.UTC().Format("2006-01-02T15:04:05.000000000Z") + "/" + string(job.Event.ID) + "/" + string(job.Event.OwnerID)
	if err := tx.Put(ctx, "agent.jobs", persistence.Key(job.Event.OwnerID, job.Event.ID), job); err != nil {
		return err
	}
	if job.Event.TaskID != "" {
		sum := sha256.Sum256([]byte(fmt.Sprintf("plan-execution/%s/%s/%d", job.Event.OwnerID, job.Event.ID, job.Version)))
		at := job.Event.CreatedAt
		if at.IsZero() {
			at = time.Unix(0, 0).UTC()
		}
		if err := realtime.NewStore(tx.Store()).Append(ctx, realtime.Visible{ID: core.ID(hex.EncodeToString(sum[:])), OwnerID: job.Event.OwnerID, Topic: "plans.execution", Object: core.Object{Kind: "assistant", ID: job.Event.AssistantID}, CreatedAt: at}); err != nil {
			return err
		}
	}
	if err := tx.NotifyAfterCommit(ctx, DispatchSignal()); err != nil {
		return err
	}
	return tx.NotifyAfterCommit(ctx, AssistantInputSignal(job.Event.OwnerID, job.Event.AssistantID))
}
