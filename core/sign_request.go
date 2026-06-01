// Provides: the canonical session-signing helper for outbound proxy /
// session-auth requests.
//
// The session headers emitted are the wire contract the ISA Platform
// session verifier admits (shared/go/auth/session/verifier.go):
//
//	Authorization:     Bearer <sessionSecret>
//	X-Isa-Session-Id:  <sessionId>
//	X-Device-ID:       <deviceId>
//	X-Isa-Timestamp:   <iso8601_z>
//	X-Isa-Signature:   hex(HMAC-SHA256(sessionSecret, canonical))
//
// The canonical string is byte-identical to session.CanonicalString in
// the server package:
//
//	<METHOD>\n<path>\n<hex(sha256(body))>\n<timestamp>\n<sessionId>
//
// No trailing newline. The Go server-side implementation is the source
// of truth; this client-side helper mirrors it.

package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Header names produced by SignRequest. Exported so callers don't
// rebuild the literal in five places.
const (
	HeaderAuthorization = "Authorization"
	HeaderIsaSessionID  = "X-Isa-Session-Id"
	HeaderDeviceID      = "X-Device-ID"
	HeaderIsaTimestamp  = "X-Isa-Timestamp"
	HeaderIsaSignature  = "X-Isa-Signature"
	BearerAuthScheme    = "Bearer"
	emptyBodySHA256Hex  = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// Errors surfaced by SignRequest.
var (
	ErrEmptySessionID     = errors.New("sign_request: sessionId must be a non-empty string")
	ErrEmptySessionSecret = errors.New("sign_request: sessionSecret must be a non-empty string")
)

// SignRequestInput groups the fields needed to compute a signed request.
// Callers populate Now to pin time in tests; the zero value falls back
// to time.Now().UTC().
type SignRequestInput struct {
	Method        string
	Path          string
	Body          []byte
	SessionID     string
	SessionSecret string
	DeviceID      string
	Now           time.Time
}

// SignedHeaders is the header bundle emitted by SignRequest.
type SignedHeaders struct {
	Authorization string
	IsaSessionID  string
	DeviceID      string
	IsaTimestamp  string
	IsaSignature  string
}

// AsMap returns the headers as a map keyed by canonical header names.
// Useful when passing into http.Header or similar consumers.
func (h SignedHeaders) AsMap() map[string]string {
	headers := map[string]string{
		HeaderAuthorization: h.Authorization,
		HeaderIsaSessionID:  h.IsaSessionID,
		HeaderIsaTimestamp:  h.IsaTimestamp,
		HeaderIsaSignature:  h.IsaSignature,
	}
	if h.DeviceID != "" {
		headers[HeaderDeviceID] = h.DeviceID
	}
	return headers
}

// FormatTimestamp renders t in UTC, RFC 3339 with a Z suffix, no
// fractional seconds. Matches Go's time.RFC3339 format for whole-second
// instants.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// CanonicalString returns the canonical signing string. Exported for
// cross-SDK byte-parity tests; production callers should use SignRequest.
func CanonicalString(method, path string, body []byte, timestamp, sessionID string) string {
	var bodyHashHex string
	if len(body) == 0 {
		bodyHashHex = emptyBodySHA256Hex
	} else {
		sum := sha256.Sum256(body)
		bodyHashHex = hex.EncodeToString(sum[:])
	}
	return strings.Join([]string{
		strings.ToUpper(method),
		path,
		bodyHashHex,
		timestamp,
		sessionID,
	}, "\n")
}

// SignRequest computes the canonical session-auth headers for a single
// outbound request. A zero Now value uses time.Now().UTC().
func SignRequest(input SignRequestInput) (SignedHeaders, error) {
	if input.SessionID == "" {
		return SignedHeaders{}, ErrEmptySessionID
	}
	if input.SessionSecret == "" {
		return SignedHeaders{}, ErrEmptySessionSecret
	}

	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	timestamp := FormatTimestamp(now)
	canonical := CanonicalString(
		input.Method,
		input.Path,
		input.Body,
		timestamp,
		input.SessionID,
	)
	mac := hmac.New(sha256.New, []byte(input.SessionSecret))
	if _, err := mac.Write([]byte(canonical)); err != nil {
		return SignedHeaders{}, fmt.Errorf("sign_request: hmac write %s %s: %w",
			input.Method, input.Path, err)
	}
	signature := hex.EncodeToString(mac.Sum(nil))

	return SignedHeaders{
		Authorization: BearerAuthScheme + " " + input.SessionSecret,
		IsaSessionID:  input.SessionID,
		DeviceID:      input.DeviceID,
		IsaTimestamp:  timestamp,
		IsaSignature:  signature,
	}, nil
}
