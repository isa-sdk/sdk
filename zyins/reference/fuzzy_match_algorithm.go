package reference

// FuzzyMatchAlgorithm — typo-tolerant text → single candidate resolution.
//
// An opt-in [MatchAlgorithm] that recovers misspellings the
// [DefaultMatchAlgorithm] (exact MakeKey + word-order-invariant checkKey)
// cannot. The default stays the default; install this via
// [WithMatchAlgorithm] to opt in. Ported from the SDK's TypeScript
// reference (packages/ts/src/zyins/reference/FuzzyMatchAlgorithm.ts).
//
// Pipeline — a tiered cascade, ranked by TIER FIRST, then by candidate
// frequency (successive tie-break, NOT one blended score):
//
//  1. exact    — MakeKey id/name equality (identity-preserving)
//  2. prefix   — candidate key starts with the query key (or vice versa)
//  3. damerau  — OSA edit distance within a length-scaled band
//     (sertaline → sertraline, chrons → crohns)
//  4. phonetic — Double Metaphone primary-code equality
//     (tylonol → tylenol, both encode TLNL)
//  5. synonym  — alias equality; [CandidateConcept] exposes no aliases
//     today, so this tier is inert (kept for cross-language parity)
//
// The first non-empty tier wins; within it the best candidate is chosen by
// lowest edit distance, then highest frequency, then a deterministic
// name/id tie-break so results are reproducible across the language ports.
//
// Parity-hardening rules (the cross-language determinism contract):
//  1. Query and every candidate string are NFC-normalized before any
//     comparison.
//  2. Lowercasing is locale-invariant (ASCII fold via strings.ToLower).
//  3. When tier + edit distance + frequency are all equal, ties break by
//     normalized candidate name, then id — stable, reproducible output.
//  4. The Double Metaphone encoder is validated against the shared vector
//     fixture reused by every port (see doublemetaphone_test.go).
//
// Safe for concurrent use: the instance holds no mutable per-call state.

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// FuzzyMatchAlgorithmOption configures a [FuzzyMatchAlgorithm].
type FuzzyMatchAlgorithmOption func(*fuzzyMatchAlgorithmOptions)

// WithFuzzyFrequencies supplies the per-id popularity map (candidate ID →
// aggregate count) that drives the intra-tier frequency tie-break. Omit it
// to disable frequency ranking; ties then fall through to the
// deterministic name/id order. The map is copied defensively, so callers
// may mutate their copy afterward without affecting the matcher.
func WithFuzzyFrequencies(frequencies map[string]int) FuzzyMatchAlgorithmOption {
	return func(o *fuzzyMatchAlgorithmOptions) {
		copied := make(map[string]int, len(frequencies))
		for id, count := range frequencies {
			copied[id] = count
		}
		o.frequencies = copied
	}
}

// WithFuzzyVersionTag stamps the matcher with a caller-supplied identifier
// reachable via [FuzzyMatchAlgorithm.VersionTag].
func WithFuzzyVersionTag(tag string) FuzzyMatchAlgorithmOption {
	return func(o *fuzzyMatchAlgorithmOptions) {
		o.versionTag = tag
	}
}

type fuzzyMatchAlgorithmOptions struct {
	frequencies map[string]int
	versionTag  string
}

// FuzzyMatchAlgorithm is the tiered typo-tolerant matcher. Construct it
// with [NewFuzzyMatchAlgorithm] and install via [WithMatchAlgorithm].
type FuzzyMatchAlgorithm struct {
	frequencies map[string]int
	versionTag  string
}

// NewFuzzyMatchAlgorithm constructs the tiered fuzzy matcher.
func NewFuzzyMatchAlgorithm(opts ...FuzzyMatchAlgorithmOption) *FuzzyMatchAlgorithm {
	resolved := fuzzyMatchAlgorithmOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return &FuzzyMatchAlgorithm{
		frequencies: resolved.frequencies,
		versionTag:  resolved.versionTag,
	}
}

// VersionTag returns the caller-supplied identifier.
func (a *FuzzyMatchAlgorithm) VersionTag() string { return a.versionTag }

// Clone returns a new matcher with the supplied options applied on top of
// this instance's configuration.
func (a *FuzzyMatchAlgorithm) Clone(opts ...FuzzyMatchAlgorithmOption) *FuzzyMatchAlgorithm {
	resolved := fuzzyMatchAlgorithmOptions{
		frequencies: a.frequencies,
		versionTag:  a.versionTag,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return &FuzzyMatchAlgorithm{
		frequencies: resolved.frequencies,
		versionTag:  resolved.versionTag,
	}
}

// scoredCandidate pairs a candidate with its match tier edit distance,
// pre-tie-break.
type scoredCandidate struct {
	candidate CandidateConcept
	distance  int
}

// Match implements [MatchAlgorithm]. It never returns an error — an
// unmatched query yields MatchResult{Found: false}.
func (a *FuzzyMatchAlgorithm) Match(query string, candidates []CandidateConcept) MatchResult {
	queryKey := makeKey(query)
	if queryKey == "" {
		return MatchResult{}
	}

	tier := a.firstNonEmptyTier(query, queryKey, candidates)
	if tier == nil {
		return MatchResult{}
	}
	winner, ok := a.bestInTier(tier)
	if !ok {
		return MatchResult{}
	}
	return MatchResult{Found: true, Candidate: winner}
}

// firstNonEmptyTier evaluates tiers in order and returns the scored
// candidates of the first tier with any hit, or nil if every tier is
// empty.
func (a *FuzzyMatchAlgorithm) firstNonEmptyTier(
	query, queryKey string,
	candidates []CandidateConcept,
) []scoredCandidate {
	if exact := collectExact(queryKey, candidates); len(exact) > 0 {
		return exact
	}
	if prefix := collectPrefix(queryKey, candidates); len(prefix) > 0 {
		return prefix
	}
	if damerau := collectDamerau(queryKey, candidates); len(damerau) > 0 {
		return damerau
	}
	if phonetic := collectPhonetic(query, candidates); len(phonetic) > 0 {
		return phonetic
	}
	if synonym := collectSynonym(query, candidates); len(synonym) > 0 {
		return synonym
	}
	return nil
}

// bestInTier picks the single winner within a tier: lowest edit distance,
// then highest frequency, then the deterministic name/id tie-break.
func (a *FuzzyMatchAlgorithm) bestInTier(tier []scoredCandidate) (CandidateConcept, bool) {
	var best scoredCandidate
	haveBest := false
	for _, c := range tier {
		if !haveBest || a.outranks(c, best) {
			best = c
			haveBest = true
		}
	}
	return best.candidate, haveBest
}

func (a *FuzzyMatchAlgorithm) outranks(x, y scoredCandidate) bool {
	if x.distance != y.distance {
		return x.distance < y.distance
	}
	xFreq := a.frequencyOf(x.candidate)
	yFreq := a.frequencyOf(y.candidate)
	if xFreq != yFreq {
		return xFreq > yFreq
	}
	return compareForTieBreak(x.candidate, y.candidate) < 0
}

func (a *FuzzyMatchAlgorithm) frequencyOf(candidate CandidateConcept) int {
	if candidate.ID == "" {
		return 0
	}
	return a.frequencies[candidate.ID]
}

// normalizeForCompare NFC-normalizes then locale-invariantly lowercases.
// Rule 1 + Rule 2 of the parity-hardening contract: café (precomposed) and
// café (decomposed) collapse to the same string before any comparison.
func normalizeForCompare(text string) string {
	return strings.ToLower(norm.NFC.String(text))
}

func collectExact(queryKey string, candidates []CandidateConcept) []scoredCandidate {
	var hits []scoredCandidate
	for _, c := range candidates {
		if makeKey(c.Name) == queryKey {
			hits = append(hits, scoredCandidate{candidate: c})
			continue
		}
		if c.ID != "" && makeKey(c.ID) == queryKey {
			hits = append(hits, scoredCandidate{candidate: c})
		}
	}
	return hits
}

func collectPrefix(queryKey string, candidates []CandidateConcept) []scoredCandidate {
	var hits []scoredCandidate
	for _, c := range candidates {
		nameKey := makeKey(c.Name)
		if nameKey == queryKey {
			continue // exact, not prefix
		}
		if strings.HasPrefix(nameKey, queryKey) || strings.HasPrefix(queryKey, nameKey) {
			hits = append(hits, scoredCandidate{
				candidate: c,
				distance:  abs(len(nameKey) - len(queryKey)),
			})
		}
	}
	return hits
}

func collectDamerau(queryKey string, candidates []CandidateConcept) []scoredCandidate {
	threshold := fuzzyThresholdForLength(len(queryKey))
	var hits []scoredCandidate
	for _, c := range candidates {
		nameKey := makeKey(c.Name)
		if nameKey == "" || nameKey == queryKey {
			continue
		}
		distance := optimalStringAlignmentDistance(queryKey, nameKey, threshold)
		if distance <= threshold {
			hits = append(hits, scoredCandidate{candidate: c, distance: distance})
		}
	}
	return hits
}

func collectPhonetic(query string, candidates []CandidateConcept) []scoredCandidate {
	queryCode := doubleMetaphone(normalizeForCompare(query)).primary
	if queryCode == "" {
		return nil
	}
	var hits []scoredCandidate
	for _, c := range candidates {
		if doubleMetaphone(normalizeForCompare(c.Name)).primary == queryCode {
			hits = append(hits, scoredCandidate{candidate: c})
		}
	}
	return hits
}

// collectSynonym is the alias tier. [CandidateConcept] surfaces no aliases
// today, so it is inert; it is kept so the cross-language contract is one
// signature rather than a future breaking change.
func collectSynonym(_ string, _ []CandidateConcept) []scoredCandidate {
	return nil
}

// compareForTieBreak is Rule 3: deterministic final tie-break by
// normalized name, then id. Empty ids sort last so the order is total.
// Uses byte-wise comparison (not locale-aware) for cross-language parity.
func compareForTieBreak(x, y CandidateConcept) int {
	xName := normalizeForCompare(x.Name)
	yName := normalizeForCompare(y.Name)
	if xName < yName {
		return -1
	}
	if xName > yName {
		return 1
	}
	xID := x.ID
	if xID == "" {
		xID = dmTieBreakIDPlaceholder
	}
	yID := y.ID
	if yID == "" {
		yID = dmTieBreakIDPlaceholder
	}
	if xID < yID {
		return -1
	}
	if xID > yID {
		return 1
	}
	return 0
}
