// Package reference — typed catalog access for the ISA zyins SDK.
//
// The reference package gives consumers a Concept handle for any
// medication, condition, or unknown free-text term. Symmetric accessors
// (Concept.Conditions, Concept.Medications) walk the v3 id-keyed maps
// directly — no client-side key normalization, no client-side sort
// heuristics.
//
// Load-bearing invariants:
//   - makeKey is INTERNAL to Match. Consumers never compute keys
//     themselves.
//   - Match never returns an error. Unknown text returns a Concept with
//     IsKnown()==false, accessors return empty slices, and InputText()
//     preserves the original string.
//   - Lookups use the server's id-keyed maps; the SDK does not re-derive
//     keys client-side.
//
// See packages/ts/src/zyins/reference.ts for the binding reference
// implementation and shared/schemas/sdk/testdata/reference_vectors.json
// for the cross-language parity corpus.
package reference

// Sort is the namespaced sort enum for Concept accessors. Members:
// SortMostCommonFirst, SortAlphabetical. No asc/desc, no closures.
// New sort orders ship as new enum members.
type Sort string

const (
	// SortMostCommonFirst orders by descending prescription frequency
	// from the v3 frequency_graphs.use_map. Ties preserve input order.
	SortMostCommonFirst Sort = "most_common_first"
	// SortAlphabetical orders by display name. Ties preserve input
	// order.
	SortAlphabetical Sort = "alphabetical"
)
