package zyins

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// idempotencyKeyLen is the byte length of the random portion of a
// generated Idempotency-Key. 16 bytes (128 bits) collides at the same
// odds as a UUID v4.
const idempotencyKeyLen = 16

// generateIdempotencyKey returns a random hex-encoded key suitable for
// the Idempotency-Key header. Random rather than derived because the
// SDK does not have a session identifier to anchor a deterministic
// derivation; callers needing replay-stable keys override via
// WithIdempotencyKey.
func generateIdempotencyKey() (string, error) {
	buf := make([]byte, idempotencyKeyLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("zyins: failed to generate idempotency key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// deriveIdempotencyKey returns a deterministic key over (token, op,
// body). Used internally by replay-style helpers and exercised by the
// package's own tests; not part of the public SDK surface. Promote to
// an exported symbol if and when an external caller needs replay-stable
// keys.
func deriveIdempotencyKey(tokenScope, op string, body []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(tokenScope))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(op))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
