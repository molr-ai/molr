// Package channelstate shares credential envelopes and connection notifications.
// Channels retain protocol state, credential encoding and associated-data rules.
package channelstate

import (
	"crypto/cipher"
	"crypto/rand"
	"molr.ai/runtime/internal/core"
)

// Seal prefixes a fresh nonce. The caller owns plaintext encoding and erasure.
func Seal(c cipher.AEAD, plaintext []byte, aad string) ([]byte, error) {
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.Seal(nonce, nonce, plaintext, []byte(aad)), nil
}

// Open preserves the caller's truncated-envelope error and the cipher's
// authentication error. The caller must clear returned plaintext after use.
func Open(c cipher.AEAD, envelope []byte, aad, truncatedMessage string) ([]byte, error) {
	n := c.NonceSize()
	if len(envelope) < n {
		return nil, core.NewError(core.Internal, truncatedMessage)
	}
	return c.Open(nil, envelope[:n], envelope[n:], []byte(aad))
}
