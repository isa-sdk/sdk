// Package zyins — v3 facade routing for the Prequalify and Quote surfaces.
//
// The per-surface APIVersion map shipped in #364 declared "v3" as a valid
// override for the prequalify and quote surfaces, but the V1/legacy
// PrequalifyService and QuoteService still always posted to their hardcoded
// paths and the PrequalifyV3Service / QuoteV3Service ran unconditionally
// regardless of the pinned version. This file wires the missing guards:
//
//   - Calling Client.PrequalifyV3.Run when the client is NOT pinned to
//     v3 on the prequalify surface fails fast with *ConfigError, matching
//     assertPrequalifyApiVersion on the TS facade (PR #377).
//   - Calling Client.QuoteV3.Run when the client is NOT pinned to v3 on
//     the quote surface fails fast with the same guard.
//   - Calling Client.Prequalify.Run / Client.Quote.Run when the client IS
//     pinned to v3 fails fast with *ConfigError directing the caller to
//     PrequalifyV3 / QuoteV3 — the v3 request type differs structurally
//     from the v1/v2 PrequalifyInput / QuoteInput, so the routing surfaces
//     as a typed guard rather than implicit dispatch (Go's equivalent of
//     the TS selector pattern, which unions the input type).
//
// The default (no APIVersion override) leaves every existing call site
// untouched: PrequalifyService and QuoteService keep their current
// behavior, and PrequalifyV3 / QuoteV3 fail clearly with a setup-pointing
// error.
//
// BundledAPIVersions stays at v2 for prequalify and quote — the default
// flip to v3 is deferred to Phase 5 of the v3 freeze plan per the locked
// SDK syntax spec (docs/sdk-syntax-proposal.md §2.7).

package zyins

// surfacePrequalify is the BundledAPIVersions / overrides key for the
// prequalify surface. Declared as a constant so the routing guards and
// any future per-surface URL builders share one source of truth.
const surfacePrequalify = "prequalify"

// surfaceQuote is the BundledAPIVersions / overrides key for the quote
// surface.
const surfaceQuote = "quote"

// apiVersionV3 is the wire token for the v3 API surface.
const apiVersionV3 = "v3"

// assertPrequalifyAPIVersion returns a *ConfigError when the client's
// resolved APIVersion for the prequalify surface does not match expected.
// Returns nil on match (including the zero-error happy path).
//
// methodName is included in the error so the caller sees which entrypoint
// rejected — e.g. "PrequalifyV3.Run" or "Prequalify.Run".
func assertPrequalifyAPIVersion(c *Client, expected, methodName string) error {
	return assertSurfaceAPIVersion(c, surfacePrequalify, expected, methodName)
}

// assertQuoteAPIVersion mirrors assertPrequalifyAPIVersion for the quote
// surface.
func assertQuoteAPIVersion(c *Client, expected, methodName string) error {
	return assertSurfaceAPIVersion(c, surfaceQuote, expected, methodName)
}

// assertSurfaceAPIVersion is the shared implementation. Kept private so
// the per-surface helpers stay the public asymmetry point — every new
// surface that grows a v3 entrypoint should add its own typed assert
// rather than expose the generic one to callers.
func assertSurfaceAPIVersion(c *Client, surface, expected, methodName string) error {
	if c == nil {
		return &ConfigError{
			Factory: methodName,
			Detail:  "zyins: client is nil; cannot resolve APIVersion",
		}
	}
	actual := c.APIVersionFor(surface)
	if actual == expected {
		return nil
	}
	if actual == "" {
		return &ConfigError{
			Factory: methodName,
			Detail: "zyins: " + methodName + " requires APIVersion " + expected +
				" on the " + surface + " surface, but no version is resolved for " +
				surface + " on this client (unknown surface)",
		}
	}
	return &ConfigError{
		Factory: methodName,
		Detail: "zyins: " + methodName + " requires APIVersion " + expected +
			" on the " + surface + " surface, but this client is pinned to " +
			actual + "; pass WithAPIVersionOverrides(map[string]string{\"" +
			surface + "\": \"" + expected + "\"}) to NewClient to opt in",
	}
}

// assertPrequalifyNotV3 returns a *ConfigError when the client is pinned
// to v3 on the prequalify surface — the caller must use
// Client.PrequalifyV3.Run with the v3 request shape instead of the
// v1/v2-shaped Client.Prequalify.Run. Returns nil for v1/v2 / unset.
func assertPrequalifyNotV3(c *Client, methodName string) error {
	return assertSurfaceNotV3(c, surfacePrequalify, methodName, "Client.PrequalifyV3.Run")
}

// assertQuoteNotV3 mirrors assertPrequalifyNotV3 for the quote surface.
func assertQuoteNotV3(c *Client, methodName string) error {
	return assertSurfaceNotV3(c, surfaceQuote, methodName, "Client.QuoteV3.Run")
}

// assertSurfaceNotV3 is the shared implementation for the inverse guard.
func assertSurfaceNotV3(c *Client, surface, methodName, redirect string) error {
	if c == nil {
		return nil
	}
	if c.APIVersionFor(surface) != apiVersionV3 {
		return nil
	}
	return &ConfigError{
		Factory: methodName,
		Detail: "zyins: " + methodName + " is the v1/v2 entrypoint for the " +
			surface + " surface, but this client is pinned to v3; call " +
			redirect + " with the v3 request shape instead",
	}
}
