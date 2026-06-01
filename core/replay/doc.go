// Package replay provides a short-lived "seen-once" cache used by HMAC
// verifiers to reject signature replay within the timestamp-tolerance window.
//
// A valid HMAC signature is body-bound and timestamp-bound, but within the
// tolerance window (typically tens of seconds to a few minutes) the same tag
// can be replayed. A verifier consults the ReplayCache before accepting a
// signature: if the key was already recorded, the request is rejected with
// ErrReplay. On first-use, the verifier records the key with a TTL equal to
// the tolerance window so expired entries cannot cause false positives after
// the window has elapsed.
//
// Implementations MUST be safe for concurrent use by multiple goroutines.
//
// Fail-closed: if the cache store is unavailable, callers MUST treat the
// error as a verification failure rather than admitting the request. The
// in-memory implementation never returns an error — network-backed
// implementations (e.g. Redis) surface infrastructure failures via SeenOnce.
package replay
