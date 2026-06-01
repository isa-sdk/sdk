// Package zyins — top-level (bundleless) Match surfaces.
//
// The locked SDK contract requires three cache-backed Match facades on
// the Client itself (mirroring the TS/PHP top-level shape):
//
//	client.Zyins.Medications().Match(ctx, text) (MedicationConcept, error)
//	client.Zyins.Conditions().Match(ctx, text)  (ConditionConcept, error)
//	client.Zyins.Concepts().Match(ctx, text)    (Concept, error)
//
// These delegate to the immutable per-bundle matchers in zyins/reference
// but hide the *DatasetBundleV3 plumbing — the SDK fetches the v3 bundle
// once via DatasetsV3.Get and memoizes the derived reference.Index on
// the client. RefreshReferenceIndex invalidates the cache, forcing the
// next Match call to re-fetch.
//
// The bundle-required surface (ReferenceService.Medications/Conditions
// /Concepts → ReferenceMatcher.Match(text, bundle)) and the explicit
// reference.Index built via Client.NewReferenceIndex remain available;
// the top-level facades are sugar for the common case where the caller
// just wants the current production catalog.

package zyins

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// referenceIndexCache memoizes the cache-backed *reference.Index on the
// client. The cache is invalidated by RefreshReferenceIndex; otherwise
// the same Index is returned for every Match call.
type referenceIndexCache struct {
	mu       sync.RWMutex
	index    *reference.Index
	inflight *referenceIndexFetch
	// generation increments on every RefreshReferenceIndex. A fetch
	// captures the generation when it starts and discards its result if
	// the generation advanced while it was in flight — otherwise a fetch
	// started before a Refresh would repopulate the cache with a stale
	// index after the Refresh cleared it.
	generation uint64
}

// referenceIndexFetch coalesces concurrent first-Match calls so only one
// DatasetsV3.Get request runs at a time.
type referenceIndexFetch struct {
	done  chan struct{}
	index *reference.Index
	err   error
}

// referenceIndex returns the cached *reference.Index, fetching the v3
// dataset bundle if no cached copy is available. Concurrent callers
// coalesce onto a single in-flight request.
func (c *Client) referenceIndex(ctx context.Context) (*reference.Index, error) {
	c.refIndex.mu.RLock()
	if idx := c.refIndex.index; idx != nil {
		c.refIndex.mu.RUnlock()
		return idx, nil
	}
	c.refIndex.mu.RUnlock()

	c.refIndex.mu.Lock()
	if idx := c.refIndex.index; idx != nil {
		c.refIndex.mu.Unlock()
		return idx, nil
	}
	if f := c.refIndex.inflight; f != nil {
		c.refIndex.mu.Unlock()
		return waitReferenceIndex(ctx, f)
	}
	f := &referenceIndexFetch{done: make(chan struct{})}
	c.refIndex.inflight = f
	startGen := c.refIndex.generation
	c.refIndex.mu.Unlock()

	// Detach the fetch from the first caller's cancellation: other callers
	// coalesce onto this single request, so a cancel by the first caller
	// must not abort the shared fetch. Deadlines/values still propagate.
	idx, err := c.fetchReferenceIndex(context.WithoutCancel(ctx))
	c.refIndex.mu.Lock()
	// Only publish the result if no RefreshReferenceIndex ran while the
	// fetch was in flight; otherwise the index is stale and must be
	// dropped so the next Match re-fetches.
	if err == nil && c.refIndex.generation == startGen {
		c.refIndex.index = idx
	}
	c.refIndex.inflight = nil
	c.refIndex.mu.Unlock()
	f.index = idx
	f.err = err
	close(f.done)
	return idx, err
}

func waitReferenceIndex(ctx context.Context, f *referenceIndexFetch) (*reference.Index, error) {
	select {
	case <-f.done:
		return f.index, f.err
	case <-ctx.Done():
		return nil, fmt.Errorf("zyins: reference index fetch: %w", ctx.Err())
	}
}

func (c *Client) fetchReferenceIndex(ctx context.Context) (*reference.Index, error) {
	res, err := c.DatasetsV3.Get(ctx, DatasetsV3Options{})
	if err != nil {
		return nil, fmt.Errorf("zyins: reference index fetch: %w", err)
	}
	if res == nil || res.Bundle == nil {
		return nil, errors.New("zyins: reference index fetch: empty datasets bundle")
	}
	return c.NewReferenceIndex(res.Bundle), nil
}

// RefreshReferenceIndex invalidates the cached *reference.Index and the
// bundle-derived reference adapters (the default autocorrector). The
// next Match or Autocorrector call re-fetches the v3 dataset bundle and
// rebuilds from it. Use after the caller knows the upstream catalog
// version has changed.
//
// A fetch already in flight when Refresh runs is detached: bumping the
// generation makes that fetch discard its result, so a stale index never
// repopulates the cache after a refresh.
func (c *Client) RefreshReferenceIndex() {
	c.refIndex.mu.Lock()
	c.refIndex.index = nil
	c.refIndex.generation++
	c.refIndex.mu.Unlock()
	c.invalidateAutocorrectorCache()
}

// MedicationsMatcher is the cache-backed bundleless matcher returned by
// Client.Medications. Match resolves free text against the current v3
// medication catalog and returns a typed reference.MedicationConcept on
// a hit, or an unknown handle (cast through reference.MedicationConcept's
// type assertion is not possible — see Match docs) on a miss.
type MedicationsMatcher struct {
	client *Client
}

// Match resolves text against the medication catalog. On a hit, returns
// a reference.MedicationConcept. On a miss, returns nil with a nil error
// — callers MUST nil-check the returned MedicationConcept. The error
// channel is reserved for transport failures fetching the v3 bundle on
// the first call.
//
// Rationale for nil-on-miss (vs. an unknown handle): the static return
// type is reference.MedicationConcept (a marker interface); an unknown
// handle cannot satisfy it without lying about Kind(). Callers who want
// the unknown handle surface should use Client.Concepts().Match which
// returns the wider reference.Concept interface.
func (m *MedicationsMatcher) Match(ctx context.Context, text string) (reference.MedicationConcept, error) {
	idx, err := m.client.referenceIndex(ctx)
	if err != nil {
		return nil, err
	}
	concept := idx.Medications.Match(text)
	if !concept.IsKnown() {
		return nil, nil
	}
	med, ok := concept.(reference.MedicationConcept)
	if !ok {
		// A known concept from Medications.Match is always a
		// MedicationConcept by construction; this branch exists only as
		// a guard against future refactors.
		return nil, nil
	}
	return med, nil
}

// ConditionsMatcher is the cache-backed bundleless matcher returned by
// Client.Conditions.
type ConditionsMatcher struct {
	client *Client
}

// Match resolves text against the condition catalog. Returns nil on a
// miss (see MedicationsMatcher.Match for rationale).
func (m *ConditionsMatcher) Match(ctx context.Context, text string) (reference.ConditionConcept, error) {
	idx, err := m.client.referenceIndex(ctx)
	if err != nil {
		return nil, err
	}
	concept := idx.Conditions.Match(text)
	if !concept.IsKnown() {
		return nil, nil
	}
	cond, ok := concept.(reference.ConditionConcept)
	if !ok {
		return nil, nil
	}
	return cond, nil
}

// ConceptsMatcher is the cache-backed bundleless matcher returned by
// Client.Concepts.
type ConceptsMatcher struct {
	client *Client
}

// Match resolves text without a kind constraint. Unlike the per-kind
// matchers, the returned reference.Concept handle preserves the unknown
// case: callers always receive a non-nil handle and branch on IsKnown().
func (m *ConceptsMatcher) Match(ctx context.Context, text string) (reference.Concept, error) {
	idx, err := m.client.referenceIndex(ctx)
	if err != nil {
		return nil, err
	}
	return idx.Concepts.Match(text), nil
}

// Medications returns the cache-backed top-level medication matcher.
// First call (or first call after RefreshReferenceIndex) fetches the v3
// dataset bundle; subsequent calls reuse the memoized *reference.Index.
func (c *Client) Medications() *MedicationsMatcher {
	return &MedicationsMatcher{client: c}
}

// Conditions returns the cache-backed top-level condition matcher.
func (c *Client) Conditions() *ConditionsMatcher {
	return &ConditionsMatcher{client: c}
}

// Concepts returns the cache-backed top-level kind-agnostic matcher.
func (c *Client) Concepts() *ConceptsMatcher {
	return &ConceptsMatcher{client: c}
}
