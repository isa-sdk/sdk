package replay

import (
	"context"
	"errors"
)

// ErrReplay signals that a signature key was already recorded within its
// replay window. Verifiers MUST map this to an authentication failure
// (HTTP 401 with a stable error code such as "replay_detected").
var ErrReplay = errors.New("replay: signature already used within window")

// Cache records signature identifiers that have been seen within a replay
// window. SeenOnce is the single operation callers use: it atomically checks
// whether key was seen and, on first use, records it with TTL equal to the
// caller-supplied window.
//
// The key is the verifier's choice — typically a tuple such as
// (sessionId, timestamp, tag) for Algosure or
// (keycode, deviceId, timestamp, tag) for LicenseHMAC. Keys SHOULD be
// high-entropy so colliding keys across unrelated requests are impossible.
//
// Implementations MUST be safe for concurrent use.
type Cache interface {
	// SeenOnce reports whether key has already been recorded within its
	// window. On first call it records key and returns (false, nil).
	// On subsequent calls within the window it returns (true, nil).
	//
	// A non-nil error indicates the cache backend is unavailable. Callers
	// MUST treat this as a verification failure (fail-closed); admitting
	// the request would reopen the replay vulnerability the cache exists
	// to close.
	SeenOnce(ctx context.Context, key string) (seen bool, err error)
}
