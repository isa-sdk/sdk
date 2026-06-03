package zyins

// De-versioned canonical SDK surface.
//
// The public SDK call site is unversioned: a consumer writes
// PrequalifyRequest / QuoteRequest and calls Client.Prequalify.Run /
// Client.Quote.Run, and the SDK routes to whichever /vN the
// BundledAPIVersions table (or a per-instance pin) selects for that
// surface. The wire version never leaks into the symbol the consumer
// types — see api/guides/api-version-pinning.md ("your call site never
// needs to track V3 suffixes").
//
// These aliases make the canonical names the primary spelling while the
// V3-suffixed names remain valid for source compatibility. They are
// plain type aliases (identical types), so a value built with one name
// is assignable to the other with no conversion — the Go stdlib pattern
// used when context moved from golang.org/x/net/context into the
// standard library.

// PrequalifyRequest is the typed request shape for the prequalify
// surface. It carries the applicant, the requested coverage, and the
// product selection to evaluate.
type PrequalifyRequest = PrequalifyV3Request

// PrequalifyOptions carries the optional request controls for a
// prequalify call (product-class filters, rank floor, ineligible
// inclusion).
type PrequalifyOptions = PrequalifyV3Options

// QuoteRequest is the typed request shape for the quote surface — the
// same applicant/coverage/products shape as PrequalifyRequest.
type QuoteRequest = QuoteV3Request

// QuoteOptions carries the optional request controls for a quote call.
type QuoteOptions = QuoteV3Options
