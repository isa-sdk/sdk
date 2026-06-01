package algosure

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
)

const (
	// timeBucketSize groups timestamps into buckets for simpleKey
	// derivation. Client and server use the same bucket so minor clock
	// skew doesn't cause a mismatch.
	timeBucketSize int64 = 30_000 // 30 seconds in milliseconds

	// maxSimpleKeyLen upper-bounds the derived key length. digitSumOf on
	// an int64 bucket cannot exceed 9 * 19 = 171; the floor of 8 never
	// raises it, so 180 is a safe ceiling that lets the key live on a
	// fixed stack-backed array and avoids per-request heap allocation on
	// the verify hot path.
	maxSimpleKeyLen = 180
)

// deriveSimpleKey writes the bucketed key into dst (sized to the derived
// length) matching the client-side derivation in
// secure-validation-library.js. Callers pass a stack-backed array to avoid
// heap allocation on the hot path.
func deriveSimpleKey(dst *[maxSimpleKeyLen]byte, salt []byte, tsMillis int64) []byte {
	if len(salt) == 0 {
		return nil
	}
	bucket := tsMillis / timeBucketSize
	keyLen := min(max(8, digitSumOf(bucket)), maxSimpleKeyLen)
	start := int(bucket % int64(len(salt)))
	if start < 0 {
		start += len(salt)
	}
	for i := range keyLen {
		idx := (start + i) % len(salt)
		if idx < 0 {
			idx += len(salt)
		}
		dst[i] = salt[idx]
	}
	return dst[:keyLen]
}

// digitSumOf returns the decimal digit sum of n, or 1 when n has no digits,
// so callers never receive a zero-length key.
func digitSumOf(n int64) int {
	if n < 0 {
		n = -n
	}
	sum := 0
	for n > 0 {
		sum += int(n % 10)
		n /= 10
	}
	if sum == 0 {
		return 1
	}
	return sum
}

// computeBodyHash returns hex(sha256(body)) and restores r.Body so downstream
// handlers can re-read it.
func computeBodyHash(r *http.Request) string {
	h := sha256.New()
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			h.Write(body)
			r.Body = io.NopCloser(strings.NewReader(string(body)))
		} else {
			r.Body = io.NopCloser(strings.NewReader(""))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// computeHMAC returns hex-encoded HMAC-SHA256 of message keyed by key.
func computeHMAC(key []byte, message string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
