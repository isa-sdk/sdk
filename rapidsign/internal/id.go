package internal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// IDSource generates opaque identifiers (UUIDv4 for idempotency keys,
// 16-byte hex for session ids). Production wires crypto/rand; tests
// inject a deterministic byte source.
type IDSource struct {
	Reader io.Reader
}

// RealIDSource returns an IDSource backed by crypto/rand.Reader.
func RealIDSource() *IDSource { return &IDSource{Reader: rand.Reader} }

// NewUUIDv4 returns a freshly-generated UUIDv4 string in canonical
// 8-4-4-4-12 form. RFC 4122 §4.4 variant + version bits are set
// explicitly so the output is conformant regardless of byte source.
func (s *IDSource) NewUUIDv4() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(s.Reader, b[:]); err != nil {
		return "", fmt.Errorf("rapidsign: failed to read entropy for uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// NewSessionID returns a 16-byte hex-encoded session identifier. The
// rapidsign server treats session_id as an opaque string; 128 bits of
// entropy is more than sufficient for collision resistance.
func (s *IDSource) NewSessionID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(s.Reader, b[:]); err != nil {
		return "", fmt.Errorf("rapidsign: failed to read entropy for session id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
