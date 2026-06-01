// Package zyins — top-level (bundleless) reference adapter facades.
//
// Mirrors the locked SDK syntax `isa.zyins.autocorrector`,
// `isa.zyins.matcher`, and `isa.zyins.autocomplete` from the cross-
// language reference: each accessor lazily fetches the v3 datasets
// bundle on first use, derives the typed adapter from it, and caches
// the result on the client.
//
// Callers who want a different algorithm inject it via the top-level
// [sdk.WithAutocorrector] / [sdk.WithMatchAlgorithm] /
// [sdk.WithAutocompleteAlgorithm] options at constructor time.

package zyins

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// referenceAdapters carries the per-client reference-namespace adapter
// overrides and the lazily-initialized defaults. Lives on *Client; one
// instance per client.
type referenceAdapters struct {
	mu                            sync.RWMutex
	autocorrectorOverride         reference.Autocorrector
	matchAlgorithmOverride        reference.MatchAlgorithm
	autocompleteAlgorithmOverride reference.AutocompleteAlgorithm

	autocorrector         reference.Autocorrector
	matchAlgorithm        reference.MatchAlgorithm
	autocompleteAlgorithm reference.AutocompleteAlgorithm
}

// SetAutocorrector installs a caller-supplied [reference.Autocorrector]
// as the value returned by Autocorrector(). Pass nil to clear the
// override and fall back to the default (built from the v3
// spelling_corrections dataset).
//
// Concurrency: SetAutocorrector takes the same lock as the lazy
// initializer; reading via Autocorrector() after this returns observes
// the new value.
func (c *Client) SetAutocorrector(a reference.Autocorrector) {
	c.adapters.mu.Lock()
	c.adapters.autocorrectorOverride = a
	c.adapters.autocorrector = nil
	c.adapters.mu.Unlock()
}

// SetMatchAlgorithm installs a caller-supplied
// [reference.MatchAlgorithm]. Pass nil to clear and fall back to the
// default key-equality algorithm.
func (c *Client) SetMatchAlgorithm(m reference.MatchAlgorithm) {
	c.adapters.mu.Lock()
	c.adapters.matchAlgorithmOverride = m
	c.adapters.mu.Unlock()
}

// SetAutocompleteAlgorithm installs a caller-supplied
// [reference.AutocompleteAlgorithm]. Pass nil to clear and fall back
// to the default bucketed algorithm.
func (c *Client) SetAutocompleteAlgorithm(a reference.AutocompleteAlgorithm) {
	c.adapters.mu.Lock()
	c.adapters.autocompleteAlgorithmOverride = a
	c.adapters.mu.Unlock()
}

// Autocorrector returns the pre-bound autocorrector for this client.
// The first call fetches the v3 datasets bundle (memoized via the same
// cache used by the top-level matchers) and constructs a
// [reference.DefaultAutocorrector] keyed on the bundle's
// spelling_corrections rows. Subsequent calls return the cached
// instance until [Client.RefreshReferenceIndex] invalidates the cache.
//
// Override the default via [Client.SetAutocorrector] or the top-level
// constructor option [sdk.WithAutocorrector].
func (c *Client) Autocorrector(ctx context.Context) (reference.Autocorrector, error) {
	c.adapters.mu.RLock()
	if c.adapters.autocorrectorOverride != nil {
		out := c.adapters.autocorrectorOverride
		c.adapters.mu.RUnlock()
		return out, nil
	}
	if c.adapters.autocorrector != nil {
		out := c.adapters.autocorrector
		c.adapters.mu.RUnlock()
		return out, nil
	}
	c.adapters.mu.RUnlock()

	bundle, err := c.cachedDatasetsBundle(ctx)
	if err != nil {
		return nil, fmt.Errorf("zyins: Autocorrector: %w", err)
	}
	corrector := reference.NewDefaultAutocorrector(bundle.SpellingTypoMap())

	c.adapters.mu.Lock()
	if c.adapters.autocorrectorOverride != nil {
		out := c.adapters.autocorrectorOverride
		c.adapters.mu.Unlock()
		return out, nil
	}
	if c.adapters.autocorrector == nil {
		c.adapters.autocorrector = corrector
	}
	out := c.adapters.autocorrector
	c.adapters.mu.Unlock()
	return out, nil
}

// MatchAlgorithm returns the configured [reference.MatchAlgorithm].
// The default is [reference.NewDefaultMatchAlgorithm]; override via
// [Client.SetMatchAlgorithm].
func (c *Client) MatchAlgorithm() reference.MatchAlgorithm {
	c.adapters.mu.RLock()
	if c.adapters.matchAlgorithmOverride != nil {
		out := c.adapters.matchAlgorithmOverride
		c.adapters.mu.RUnlock()
		return out
	}
	c.adapters.mu.RUnlock()

	c.adapters.mu.Lock()
	defer c.adapters.mu.Unlock()
	if c.adapters.matchAlgorithmOverride != nil {
		return c.adapters.matchAlgorithmOverride
	}
	if c.adapters.matchAlgorithm == nil {
		c.adapters.matchAlgorithm = reference.NewDefaultMatchAlgorithm()
	}
	return c.adapters.matchAlgorithm
}

// AutocompleteAlgorithm returns the configured
// [reference.AutocompleteAlgorithm]. The default is the bucketed
// algorithm; override via [Client.SetAutocompleteAlgorithm].
func (c *Client) AutocompleteAlgorithm() reference.AutocompleteAlgorithm {
	c.adapters.mu.RLock()
	if c.adapters.autocompleteAlgorithmOverride != nil {
		out := c.adapters.autocompleteAlgorithmOverride
		c.adapters.mu.RUnlock()
		return out
	}
	c.adapters.mu.RUnlock()

	c.adapters.mu.Lock()
	defer c.adapters.mu.Unlock()
	if c.adapters.autocompleteAlgorithmOverride != nil {
		return c.adapters.autocompleteAlgorithmOverride
	}
	if c.adapters.autocompleteAlgorithm == nil {
		c.adapters.autocompleteAlgorithm = reference.NewDefaultAutocompleteAlgorithm()
	}
	return c.adapters.autocompleteAlgorithm
}

// invalidateAutocorrectorCache clears the bundle-derived default
// autocorrector so the next Autocorrector call rebuilds it from a fresh
// v3 datasets bundle. A caller-supplied override (SetAutocorrector) is
// left intact — it is not bundle-derived. Called by
// [Client.RefreshReferenceIndex]; the matchAlgorithm and
// autocompleteAlgorithm defaults are bundle-independent and need no
// invalidation.
func (c *Client) invalidateAutocorrectorCache() {
	c.adapters.mu.Lock()
	c.adapters.autocorrector = nil
	c.adapters.mu.Unlock()
}

// cachedDatasetsBundle returns the v3 bundle backing the cache-backed
// reference index. Used by Autocorrector and the future adapter facade
// surfaces.
func (c *Client) cachedDatasetsBundle(ctx context.Context) (*DatasetBundleV3, error) {
	res, err := c.DatasetsV3.Get(ctx, DatasetsV3Options{})
	if err != nil {
		return nil, err
	}
	if res == nil || res.Bundle == nil {
		return nil, errors.New("zyins: cached datasets bundle: empty")
	}
	return res.Bundle, nil
}
