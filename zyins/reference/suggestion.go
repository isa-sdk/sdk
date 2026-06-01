package reference

// Suggestion is one ranked candidate returned by an
// [AutocompleteAlgorithm]. The fields are deliberately minimal so
// custom algorithms can plug in without depending on the full Concept
// interface — the SDK's higher-level facades wrap a Suggestion into a
// Concept handle for the caller.
type Suggestion struct {
	// Concept is the matched candidate (id + display name + kind).
	Concept CandidateConcept
	// Score is the algorithm-defined ranking score; higher is better.
	// Comparison across algorithms is undefined.
	Score float64
	// MatchedSpan records the [start,end) rune-index range of the
	// query inside the candidate's display name. Both indices are 0
	// when the algorithm does not surface a span.
	MatchedSpan [2]int
	// Rank is the candidate's zero-based rank in the returned slice.
	// Set by the algorithm; consumers may read but should not mutate.
	Rank int
}
