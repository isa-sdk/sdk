package replay

import (
	"context"
	"sync"
	"time"
)

// memoryCacheMaxDefault caps the number of live entries in an
// InMemoryCache when the caller does not specify a limit. The value is
// chosen so a small single-instance deployment cannot exhaust memory even
// under a pathological burst; production deployments SHOULD size this
// explicitly via MemoryConfig.MaxEntries.
const memoryCacheMaxDefault = 100_000

// MemoryConfig configures an InMemoryCache.
type MemoryConfig struct {
	// Window is the TTL applied to each recorded key. Required; must be > 0.
	// Callers pass the verifier's replay window (e.g. 60s for Algosure,
	// 300s for LicenseHMAC). The cache rejects construction with a zero
	// window rather than silently applying a default, since a silent
	// default would hide a misconfigured verifier.
	Window time.Duration

	// MaxEntries caps live entries. Zero uses memoryCacheMaxDefault. When
	// the cap is reached, the oldest entries are evicted in insertion
	// order — correctness is preserved because expired entries are also
	// evicted and the window bounds the replay risk of any evicted key.
	MaxEntries int

	// Now returns the current time. Nil uses time.Now. Injectable for tests.
	Now func() time.Time
}

// InMemoryCache is a single-process Cache implementation backed by a map
// and an insertion-ordered queue. It is safe for concurrent use.
//
// Multi-instance deployments MUST swap this for a shared backend (e.g.
// Redis) so a replayed request landing on a different instance is still
// rejected.
type InMemoryCache struct {
	window     time.Duration
	maxEntries int
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]time.Time
	order   []string // insertion order; used for bounded eviction
}

// NewInMemoryCache constructs an InMemoryCache or returns an error if
// cfg.Window is not positive.
func NewInMemoryCache(cfg MemoryConfig) (*InMemoryCache, error) {
	if cfg.Window <= 0 {
		return nil, errInvalidWindow
	}
	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = memoryCacheMaxDefault
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &InMemoryCache{
		window:     cfg.Window,
		maxEntries: maxEntries,
		now:        now,
		entries:    make(map[string]time.Time),
	}, nil
}

// SeenOnce implements Cache.
func (c *InMemoryCache) SeenOnce(_ context.Context, key string) (bool, error) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredLocked(now)

	if exp, ok := c.entries[key]; ok && exp.After(now) {
		return true, nil
	}

	// First use: record with TTL = window.
	c.entries[key] = now.Add(c.window)
	c.order = append(c.order, key)

	if len(c.entries) > c.maxEntries {
		c.evictOldestLocked()
	}
	return false, nil
}

// evictExpiredLocked removes entries past their TTL. Runs in amortized
// O(k) per call where k is the number of newly expired items at the head
// of the insertion queue.
func (c *InMemoryCache) evictExpiredLocked(now time.Time) {
	i := 0
	for ; i < len(c.order); i++ {
		key := c.order[i]
		exp, ok := c.entries[key]
		if !ok {
			continue
		}
		if exp.After(now) {
			break
		}
		delete(c.entries, key)
	}
	if i > 0 {
		c.order = c.order[i:]
	}
}

// evictOldestLocked drops the single oldest entry to keep within MaxEntries.
func (c *InMemoryCache) evictOldestLocked() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}

var _ Cache = (*InMemoryCache)(nil)
