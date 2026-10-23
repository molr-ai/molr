package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Receipt records a request digest and its encoded result in a domain-owned
// bucket. Callers write it in the same transaction as their business changes.
type Receipt struct {
	Digest string
	Result json.RawMessage
}

// PayloadDigest hashes the existing JSON request representation. Validation and
// the account/action/idempotency-key scope remain the caller's responsibility.
func PayloadDigest(payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ReplayReceipt reads an already authorized request's result within the supplied
// transaction. Only a missing receipt returns (false, nil); any other error must
// stop the mutation. This helper performs no authorization or business writes.
func ReplayReceipt(ctx context.Context, tx Tx, bucket, key, digest string, result any) (bool, error) {
	var old Receipt
	err := tx.Get(ctx, bucket, key, &old)
	if Missing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if old.Digest != digest {
		return true, writeConflict()
	}
	return true, json.Unmarshal(old.Result, result)
}
