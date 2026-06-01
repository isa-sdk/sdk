package zyins

import (
	"context"
	"sync"
	"testing"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// raceGoroutines is the concurrency level for the -race regression
// tests. High enough that an unsynchronized read/write reliably trips
// the race detector, low enough to stay fast.
const raceGoroutines = 50

// TestResolveCaseStorage_ConcurrentInit_NoRace exercises the lazy
// zero-knowledge default construction from many goroutines at once. The
// pre-fix resolveCaseStorage read then wrote c.caseStorage without
// synchronization; under -race that read/write pair tripped the
// detector and could hand different callers different storage instances.
func TestResolveCaseStorage_ConcurrentInit_NoRace(t *testing.T) {
	t.Parallel()
	c := newCaseStorageTestClient(t)

	var wg sync.WaitGroup
	results := make([]any, raceGoroutines)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = c.resolveCaseStorage()
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("resolveCaseStorage returned nil")
	}
	for i, got := range results {
		if got != first {
			t.Fatalf("goroutine %d observed a different storage instance; lazy init is not single-flighted", i)
		}
	}
}

// TestReferenceIndex_ConcurrentFirstMatch_NoRace coalesces many
// concurrent first-Match calls onto a single dataset fetch. The shared
// fetch must not race on the cache fields and must complete even though
// callers arrive concurrently.
func TestReferenceIndex_ConcurrentFirstMatch_NoRace(t *testing.T) {
	t.Parallel()
	c, doer := newMatchTopClient(t)

	var wg sync.WaitGroup
	for i := 0; i < raceGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Medications().Match(context.Background(), "LISINOPRIL"); err != nil {
				t.Errorf("Match: %v", err)
			}
		}()
	}
	wg.Wait()

	// Coalescing means the bundle is fetched far fewer times than the
	// goroutine count; assert it was fetched at least once.
	if doer.calls.Load() == 0 {
		t.Fatal("expected at least one dataset fetch")
	}
}

// TestReferenceIndex_FirstCallerCancel_DoesNotAbortSharedFetch proves the
// coalesced fetch is detached from the first caller's cancellation while
// a second caller is genuinely joined to the same in-flight fetch.
//
// The blocking doer forces a real overlap window: caller A starts the
// coalesced fetch and the doer parks it (entered closed, blocked on
// release); caller B then takes the cache lock, observes the in-flight
// fetch, and parks in waitReferenceIndex on the shared done channel.
// Only then does caller A cancel its context. The shared fetch must
// survive that cancel — it runs on context.WithoutCancel(A.ctx) — and B
// must receive a usable index from the single fetch (doer.calls == 1).
//
// This test fails against code that passes A's cancellable context to
// the shared fetch: cancelling A would abort the doer (its select hits
// req.Context().Done()), the shared fetch would error, and B — parked on
// the same done channel — would receive that context-cancelled error.
func TestReferenceIndex_FirstCallerCancel_DoesNotAbortSharedFetch(t *testing.T) {
	t.Parallel()
	doer := newMatchTopBlockingDoer(matchTopBundleJSON)
	c, err := NewClient(WithToken("isa_test_aaaaaaaaaaaaaaaaaaaa"), WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.doer = doer

	cancelCtx, cancel := context.WithCancel(context.Background())

	// Caller A starts the coalesced fetch; the doer parks it once entered.
	aDone := make(chan struct{})
	go func() {
		defer close(aDone)
		_, _ = c.Medications().Match(cancelCtx, "LISINOPRIL")
	}()
	<-doer.entered // A's fetch is in flight; refIndex.inflight is set.

	// Caller B joins the in-flight fetch. Because inflight was set before
	// the doer signalled entered, B is guaranteed to coalesce (take the
	// cache lock, see inflight != nil, park in waitReferenceIndex on the
	// shared done channel) rather than start a second fetch.
	type matchResult struct {
		concept reference.MedicationConcept
		err     error
	}
	bResult := make(chan matchResult, 1)
	go func() {
		concept, err := c.Medications().Match(context.Background(), "LISINOPRIL")
		bResult <- matchResult{concept: concept, err: err}
	}()

	// Cancel A while the shared fetch is still parked and B is joined to
	// it. With the fix, the fetch ignores this cancel; without it, the
	// fetch aborts and B inherits the failure.
	cancel()
	close(doer.release) // let the (detached) shared fetch complete

	res := <-bResult
	<-aDone
	if res.err != nil {
		t.Fatalf("second caller Match with live ctx: %v", res.err)
	}
	if res.concept == nil || res.concept.ID() != "LISINOPRIL" {
		t.Fatalf("second caller Match returned %v, want LISINOPRIL concept", res.concept)
	}
	if got := doer.calls.Load(); got != 1 {
		t.Fatalf("dataset fetch calls = %d, want 1 (callers must coalesce onto one shared fetch)", got)
	}
}

// TestRefreshReferenceIndex_ConcurrentWithMatch_NoRace hammers
// RefreshReferenceIndex against concurrent Match calls. RefreshReference
// Index clears the index, bumps the generation, and invalidates the
// autocorrector cache; the Match path reads the same fields. Under -race
// this catches unsynchronized access and the stale-repopulation bug
// (a fetch in flight across a Refresh must not republish a stale index).
func TestRefreshReferenceIndex_ConcurrentWithMatch_NoRace(t *testing.T) {
	t.Parallel()
	c, _ := newMatchTopClient(t)

	var wg sync.WaitGroup
	for i := 0; i < raceGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Medications().Match(context.Background(), "LISINOPRIL")
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.RefreshReferenceIndex()
		}()
	}
	wg.Wait()
}

// TestRefreshReferenceIndex_InvalidatesAutocorrector proves Refresh drops
// the bundle-derived autocorrector so the next Autocorrector call
// rebuilds from a fresh fetch. Pre-fix, Refresh cleared only the index;
// the cached autocorrector survived, serving stale spelling corrections.
func TestRefreshReferenceIndex_InvalidatesAutocorrector(t *testing.T) {
	t.Parallel()
	c, doer := newMatchTopClient(t)

	if _, err := c.Autocorrector(context.Background()); err != nil {
		t.Fatalf("first Autocorrector: %v", err)
	}
	fetchesAfterFirst := doer.calls.Load()

	c.RefreshReferenceIndex()

	if _, err := c.Autocorrector(context.Background()); err != nil {
		t.Fatalf("second Autocorrector: %v", err)
	}
	if doer.calls.Load() <= fetchesAfterFirst {
		t.Fatalf("Autocorrector after Refresh reused the stale cache (fetches %d, want > %d)",
			doer.calls.Load(), fetchesAfterFirst)
	}
}
