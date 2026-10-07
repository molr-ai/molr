package trigger

import (
	"context"
	"strconv"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Occurrence is a trusted adapter identity. Kind separates internal maintenance
// from user schedules even if owner, ID, revision and wall-clock time coincide.
type Occurrence struct {
	OwnerID  core.ID
	Kind     string
	ID       core.ID
	Revision core.Version
	At       time.Time
}

// RecordOccurrence joins the caller's transaction. The caller must commit its
// cursor and business effects in that same transaction, or roll everything back.
func RecordOccurrence(ctx context.Context, store persistence.Store, v Occurrence) (inserted bool, err error) {
	if v.OwnerID.Validate() != nil || core.ID(v.Kind).Validate() != nil || v.ID.Validate() != nil || v.Revision == 0 || v.At.IsZero() {
		return false, core.Invalid("occurrence", "scoped identity, revision and due time required")
	}
	key := string(v.OwnerID) + "/" + v.Kind + "/" + string(v.ID) + "/" + strconv.FormatUint(uint64(v.Revision), 10) + "/" + v.At.UTC().Format(time.RFC3339Nano)
	err = store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old Occurrence
		if e := tx.Get(ctx, "trigger.occurrences", key, &old); e == nil {
			return nil
		} else if !persistence.Missing(e) {
			return e
		}
		if e := tx.Put(ctx, "trigger.occurrences", key, v); e != nil {
			return e
		}
		inserted = true
		return nil
	})
	return
}
