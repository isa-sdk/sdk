package zyins

import (
	"context"
	"fmt"
	"net/http"

	"github.com/isa-sdk/sdk/zyins/cases"
)

// APIVersionFor returns the per-surface API version pinned on this
// client. Per-instance overrides supplied via
// [WithAPIVersionOverrides] take precedence; otherwise the value
// falls through to [BundledAPIVersions]. Unknown surfaces return an
// empty string — callers MUST treat the empty value as a
// configuration error.
//
// Per-surface request paths consume this value at construction time.
// Integration code that builds custom paths reads it directly.
func (c *Client) APIVersionFor(surface string) string {
	return ResolveAPIVersion(c.apiVersionOverrides, surface)
}

// caseStorageDoer bridges *Client into the [cases.Doer] interface
// the [cases.ZeroKnowledgeCaseStorage] default depends on. The
// adapter keeps the cases sub-package free of a hard dependency on
// the zyins transport types.
type caseStorageDoer struct {
	client *Client
}

// Post implements [cases.Doer].
func (d caseStorageDoer) Post(ctx context.Context, path string, body any) ([]byte, error) {
	return d.client.doJSON(ctx, requestArgs{
		method: http.MethodPost,
		path:   path,
		body:   body,
		op:     "cases_put",
	})
}

// Get implements [cases.Doer].
func (d caseStorageDoer) Get(ctx context.Context, path string) ([]byte, error) {
	return d.client.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   path,
		op:     "cases_get",
	})
}

// resolveCaseStorage returns the configured [cases.CaseStorage] for
// this client, lazily constructing the zero-knowledge default if the
// caller did not override. Safe for concurrent use: the lazy default is
// built exactly once under caseStorageOnce, so concurrent Save/Recall
// calls observe a single, consistent storage instance.
func (c *Client) resolveCaseStorage() cases.CaseStorage {
	c.caseStorageOnce.Do(func() {
		if c.caseStorage == nil {
			c.caseStorage = cases.NewZeroKnowledgeCaseStorage(caseStorageDoer{client: c})
		}
	})
	return c.caseStorage
}

// Save persists a [cases.CaseRecord] via the configured
// [cases.CaseStorage]. The default storage is the zero-knowledge
// AES-256-GCM envelope; swap it via [WithCaseStorage] for callers
// that need a non-default backing store (in-memory test fake,
// integration test recorder, alternative wire endpoint).
func (s *CasesService) Save(ctx context.Context, record cases.CaseRecord) (cases.PutResult, error) {
	storage := s.client.resolveCaseStorage()
	result, err := storage.Put(ctx, record)
	if err != nil {
		return cases.PutResult{}, fmt.Errorf("zyins: Cases.Save: %w", err)
	}
	return result, nil
}

// Recall fetches and decrypts a previously-stored case via the
// configured [cases.CaseStorage]. The recall token is the value
// returned by Save; callers MUST persist it alongside the case id.
//
// Recall is the canonical verb per the locked SDK syntax (TS canon:
// isa.zyins.cases.recall).
func (s *CasesService) Recall(ctx context.Context, id, recallToken string) (cases.CaseRecord, error) {
	storage := s.client.resolveCaseStorage()
	record, err := storage.Get(ctx, id, recallToken)
	if err != nil {
		return cases.CaseRecord{}, fmt.Errorf("zyins: Cases.Recall: %w", err)
	}
	return record, nil
}

// Open fetches and decrypts a previously-stored case.
//
// Deprecated: Use Recall instead. Will be removed in v1.0.
func (s *CasesService) Open(ctx context.Context, id, recallToken string) (cases.CaseRecord, error) {
	return s.Recall(ctx, id, recallToken)
}

// Delete removes a stored case via the configured storage. Returns
// [cases.ErrUnsupported] when the configured storage does not
// implement [cases.DeleteableCaseStorage]. Stores that DO support
// delete (the zero-knowledge default does NOT today) honor the call
// directly.
func (s *CasesService) Delete(ctx context.Context, id string) error {
	storage := s.client.resolveCaseStorage()
	deleter, ok := storage.(cases.DeleteableCaseStorage)
	if !ok {
		return cases.ErrUnsupported
	}
	if err := deleter.Delete(ctx, id); err != nil {
		return fmt.Errorf("zyins: Cases.Delete: %w", err)
	}
	return nil
}
