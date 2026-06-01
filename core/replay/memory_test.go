package replay

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewInMemoryCache_RejectsZeroWindow(t *testing.T) {
	if _, err := NewInMemoryCache(MemoryConfig{}); err == nil {
		t.Fatal("expected error for zero window; got nil")
	}
}

func TestSeenOnce_FirstUseReturnsFalse(t *testing.T) {
	c := mustCache(t)
	seen, err := c.SeenOnce(context.Background(), "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen {
		t.Fatal("expected seen=false on first use")
	}
}

func TestSeenOnce_SecondUseReturnsTrue(t *testing.T) {
	c := mustCache(t)
	if _, err := c.SeenOnce(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}
	seen, err := c.SeenOnce(context.Background(), "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !seen {
		t.Fatal("expected seen=true on second use within window")
	}
}

func TestSeenOnce_ExpiredKeyAcceptsAgain(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{now: now}
	c, err := NewInMemoryCache(MemoryConfig{Window: 10 * time.Second, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.SeenOnce(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}
	clock.advance(11 * time.Second) // past window
	seen, err := c.SeenOnce(context.Background(), "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen {
		t.Fatal("expected expired key to be re-accepted; got seen=true")
	}
}

func TestSeenOnce_DistinctKeysDoNotCollide(t *testing.T) {
	c := mustCache(t)
	for i := range 100 {
		key := fmt.Sprintf("k%d", i)
		seen, err := c.SeenOnce(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		if seen {
			t.Fatalf("key %q collided with prior insert", key)
		}
	}
}

func TestSeenOnce_ConcurrentSafe(t *testing.T) {
	c := mustCache(t)
	const workers = 16
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 500 {
				_, _ = c.SeenOnce(context.Background(), fmt.Sprintf("w%d-k%d", id, i))
			}
		}(w)
	}
	wg.Wait()
}

func TestSeenOnce_BoundedByMaxEntries(t *testing.T) {
	c, err := NewInMemoryCache(MemoryConfig{Window: time.Hour, MaxEntries: 3})
	if err != nil {
		t.Fatal(err)
	}
	// Insert 5 keys; oldest 2 should be evicted.
	for i := range 5 {
		if _, err := c.SeenOnce(context.Background(), fmt.Sprintf("k%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// k0 and k1 should have been evicted; k2..k4 should still be "seen".
	for _, k := range []string{"k2", "k3", "k4"} {
		seen, _ := c.SeenOnce(context.Background(), k)
		if !seen {
			t.Fatalf("key %q should still be retained", k)
		}
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func mustCache(t *testing.T) *InMemoryCache {
	t.Helper()
	c, err := NewInMemoryCache(MemoryConfig{Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
