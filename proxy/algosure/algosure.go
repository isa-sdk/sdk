// Package algosure implements the Algosure ambient authentication verifier
// with intra-bucket replay protection.
//
// Algosure authenticates client-side applications (eApps, signer pages) that
// cannot ship a shared secret. The client derives an HMAC tag from a rotating
// public salt file hosted at the customer's domain, binding the tag to the
// specific request (method, path, body hash, timestamp, session ID).
//
// Replay protection: the verifier consults a replay.Cache before accepting
// a signature. Within the replay window (default 60s = 2x bucket), a tag
// may be used exactly once. Closes F-6 from security audit 2026-04-19.
//
// Patent: IIP-0016-WO "Methods and System to Authenticate Client-Side
// Transmission Access" — CIP pending for HMAC variant.
package algosure

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/isa-sdk/sdk/core/replay"
)

const (
	headerHost      = "*Host"
	headerTimestamp = "*Timestamp"
	headerSessionID = "*sessionId"
	headerSaltID    = "*SaltId"
	headerAuth      = "Authorization"
	headerRequestID = "X-Request-Id"

	// defaultTimestampTolerance is the maximum age of a valid Algosure
	// signature. Requests outside this window are rejected.
	defaultTimestampTolerance = 30 * time.Second
)

// ErrReplay aliases replay.ErrReplay so callers that only depend on this
// package do not also need to import replay.
var ErrReplay = replay.ErrReplay

// ErrMissingSaltID indicates the request omitted the *SaltId header (or sent
// a non-numeric value). Callers should map this to a 400-class response — it
// is a client request-shape error, not an authentication failure. Keeping it
// distinct from the generic auth-failure path lets the proxy log it
// separately and lets browsers surface a useful "rebuild the form" hint
// rather than the opaque 401 reserved for tampered tags.
var ErrMissingSaltID = errors.New("algosure: missing or invalid *SaltId header")

// AuthContext is the value returned on successful verification.
type AuthContext struct {
	AccountID string
	RequestID string
}

// HostRepository resolves a *Host value to a scope/account identifier.
type HostRepository interface {
	LookupHost(ctx context.Context, host string) (accountID string, err error)
}

// SaltFetcher retrieves a salt by its rotation ID, scope-pinned to the
// caller's host.
//
// Embed-with-id model: deployed forms carry the exact salt_id they were
// built with. proxy_salts is append-only-forever — every rotation appends
// a new row and every old row stays valid for the life of forms built
// against it. Callers therefore do an exact-match lookup by (salt_id, host)
// rather than a "latest by host" lookup; rotation in the platform proceeds
// independently of carriers holding deployed forms in compliance review.
//
// Host pin is non-negotiable: a salt_id leaked from scope A must not
// authenticate a request claiming scope B. Implementations MUST scope the
// lookup by host and return ErrSaltUnavailable (or the implementation's
// equivalent sentinel) on no-match.
type SaltFetcher interface {
	// FetchByID returns the salt_content for the given salt rotation id,
	// pinned to host. Returns the implementation's "no row" sentinel on
	// no-match. Used by the verifier on every request.
	FetchByID(ctx context.Context, saltID int64, host string) ([]byte, error)
}

// Config configures the Algosure verifier.
type Config struct {
	// Hosts resolves *Host → account ID. Required.
	Hosts HostRepository

	// Salt fetches the rotating salt from customer hosts. Required.
	Salt SaltFetcher

	// Replay stores seen signatures to reject intra-window reuse. Required —
	// callers MUST supply a cache; omitting it would reopen F-6.
	Replay replay.Cache

	// Now returns the current time. Nil uses time.Now.
	Now func() time.Time

	// TimestampTolerance overrides the default ±30s window.
	TimestampTolerance time.Duration
}

// Verifier validates Algosure HMAC signatures with replay protection.
type Verifier struct {
	hosts     HostRepository
	salt      SaltFetcher
	replay    replay.Cache
	now       func() time.Time
	tolerance time.Duration
}

// NewVerifier constructs a Verifier. Returns an error if any required
// dependency is nil. Missing Replay is an explicit error rather than a
// silent default because a nil cache reopens F-6.
func NewVerifier(cfg Config) (*Verifier, error) {
	if cfg.Hosts == nil {
		return nil, errors.New("algosure: Config.Hosts must not be nil")
	}
	if cfg.Salt == nil {
		return nil, errors.New("algosure: Config.Salt must not be nil")
	}
	if cfg.Replay == nil {
		return nil, errors.New("algosure: Config.Replay must not be nil (fail-closed: nil cache would reopen F-6)")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	tolerance := cfg.TimestampTolerance
	if tolerance < 0 {
		return nil, errors.New("algosure: Config.TimestampTolerance must not be negative")
	}
	if tolerance == 0 {
		tolerance = defaultTimestampTolerance
	}
	return &Verifier{
		hosts:     cfg.Hosts,
		salt:      cfg.Salt,
		replay:    cfg.Replay,
		now:       now,
		tolerance: tolerance,
	}, nil
}

// Supports returns true if the request carries Algosure headers.
func (v *Verifier) Supports(r *http.Request) bool {
	return r.Header.Get(headerHost) != "" && r.Header.Get(headerTimestamp) != ""
}

// Verify validates the signature, rejects replays, and returns an AuthContext.
// The function reads as a table of contents — each phase is a named helper.
func (v *Verifier) Verify(ctx context.Context, r *http.Request) (*AuthContext, error) {
	inputs, err := parseAlgosureRequest(r)
	if err != nil {
		return nil, err
	}
	if err := v.checkTimestampDrift(inputs.tsMillis); err != nil {
		return nil, err
	}
	accountID, err := v.hosts.LookupHost(ctx, inputs.host)
	if err != nil {
		return nil, fmt.Errorf("algosure: host %q not in allowlist: %w", inputs.host, err)
	}
	if err := v.verifySignature(ctx, r, inputs); err != nil {
		return nil, err
	}
	// Replay check runs AFTER signature validation so bogus tags cannot
	// pollute the cache. Fail-closed: cache errors reject the request.
	if err := v.recordOrReject(ctx, inputs); err != nil {
		return nil, err
	}
	return &AuthContext{AccountID: accountID, RequestID: r.Header.Get(headerRequestID)}, nil
}

// verifySignature fetches the salt, derives the key, and compares the HMAC
// tag against Authorization in constant time.
func (v *Verifier) verifySignature(ctx context.Context, r *http.Request, in algosureInputs) error {
	saltContent, err := v.salt.FetchByID(ctx, in.saltID, in.host)
	if err != nil {
		return fmt.Errorf("algosure: failed to fetch salt %d for host %q: %w", in.saltID, in.host, err)
	}
	if len(saltContent) == 0 {
		return fmt.Errorf("algosure: empty salt content for salt %d host %q", in.saltID, in.host)
	}
	var keyBuf [maxSimpleKeyLen]byte
	simpleKey := deriveSimpleKey(&keyBuf, saltContent, in.tsMillis)
	bodyHash := computeBodyHash(r)
	message := strings.Join([]string{
		r.Method,
		r.URL.Path,
		bodyHash,
		in.tsStr,
		in.sessionID,
	}, "\x00")
	expectedMAC := computeHMAC(simpleKey, message)
	if !hmac.Equal([]byte(in.authHeader), []byte(expectedMAC)) {
		return errors.New("algosure: HMAC verification failed")
	}
	return nil
}

// checkTimestampDrift rejects requests whose client timestamp falls outside
// the tolerance window.
func (v *Verifier) checkTimestampDrift(tsMillis int64) error {
	clientTime := time.UnixMilli(tsMillis)
	drift := v.now().Sub(clientTime)
	if drift < 0 {
		drift = -drift
	}
	if drift > v.tolerance {
		return fmt.Errorf("algosure: timestamp drift %v exceeds tolerance %v", drift, v.tolerance)
	}
	return nil
}

// recordOrReject consults the replay cache. First use wins; second use
// within the window returns ErrReplay. Cache backend errors fail-closed.
func (v *Verifier) recordOrReject(ctx context.Context, in algosureInputs) error {
	key := in.sessionID + "\x00" + in.tsStr + "\x00" + in.authHeader
	seen, err := v.replay.SeenOnce(ctx, key)
	if err != nil {
		return fmt.Errorf("algosure: replay cache unavailable (failing closed): %w", err)
	}
	if seen {
		return ErrReplay
	}
	return nil
}
