// Package cases declares the storage contract for ISA case records and
// ships the zero-knowledge default. Callers wire an alternative store
// via [LicenseOptions.CaseStorage] / [BearerOptions.CaseStorage] /
// [SessionOptions.CaseStorage] on the parent SDK package.
//
// The contract is intentionally small: [Put] and [Get]. Stores that
// also honor delete additionally implement [DeleteableCaseStorage];
// callers may type-assert (or rely on the parent SDK's wrapper to do
// so) and receive [ErrUnsupported] when the store does not.
//
// The zero-knowledge default ([ZeroKnowledgeCaseStorage]) AES-GCM-256
// encrypts the record payload client-side and POSTs only opaque
// ciphertext to the platform's /v1/cases surface. The recall token
// carries the data key in a URL-safe base64 fragment; without it the
// server cannot decrypt the record.
package cases

import (
	"context"
	"errors"
)

// CaseRecord is the unit of storage. Body is the cleartext payload the
// SDK accepts from callers; the storage implementation is responsible
// for any encryption before the value reaches a remote endpoint.
//
// ID is assigned by the storage on Put if empty on input. Product is a
// cleartext routing tag (e.g., "zyins", "eapp"); the zero-knowledge
// default binds it as AES-GCM additional authenticated data so a
// record encrypted under one product cannot be decrypted under
// another.
type CaseRecord struct {
	ID       string
	Product  string
	Body     []byte
	Metadata map[string]string
}

// PutResult is the outcome of a Put. ID is the server-assigned (or
// caller-supplied) identifier; RecallToken is the opaque value the
// caller must persist to retrieve the record later. For
// [ZeroKnowledgeCaseStorage] the recall token is the AES-GCM data
// key encoded as URL-safe base64 — losing it makes the record
// unrecoverable.
type PutResult struct {
	ID          string
	RecallToken string
}

// ErrNotFound is returned by [CaseStorage.Get] when no record exists
// for the supplied id (or when the record exists but the recall token
// is wrong). Callers MUST match on this sentinel rather than parsing
// error messages.
var ErrNotFound = errors.New("cases: record not found")

// ErrUnsupported is returned by the zyins package's Delete wrapper
// when the configured storage does not implement
// [DeleteableCaseStorage]. The sentinel lets callers detect capability
// gaps without reflection.
var ErrUnsupported = errors.New("cases: configured storage does not support delete")

// CaseStorage is the mandatory storage contract. Implementations MUST
// be safe for concurrent use; the SDK wires one storage per client
// and shares it across goroutines.
//
// Get MUST return ([CaseRecord]{}, [ErrNotFound]) on a miss — never a
// nil record, never a non-sentinel error.
type CaseStorage interface {
	Put(ctx context.Context, record CaseRecord) (PutResult, error)
	Get(ctx context.Context, id, recallToken string) (CaseRecord, error)
}

// DeleteableCaseStorage is the optional capability for stores that
// can remove a record. The zyins package's Cases.Delete wrapper
// type-asserts the configured storage; stores that do not implement
// it surface [ErrUnsupported] to the caller.
type DeleteableCaseStorage interface {
	CaseStorage
	Delete(ctx context.Context, id string) error
}

// Doer is the minimal transport contract the [ZeroKnowledgeCaseStorage]
// default depends on. The zyins package supplies an adapter wrapping
// *zyins.Client; consumers may inject a fake [Doer] in tests without
// touching the SDK's transport surface.
//
// Post sends a JSON-encoded body to path and returns the response
// envelope bytes. Get reads from path. Both return the wrapped HTTP
// error verbatim on non-2xx responses; the storage layer maps 404 to
// [ErrNotFound].
type Doer interface {
	Post(ctx context.Context, path string, body any) ([]byte, error)
	Get(ctx context.Context, path string) ([]byte, error)
}
