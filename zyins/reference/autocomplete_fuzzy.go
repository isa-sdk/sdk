package reference

// fuzzyFallbackThreshold is the substring-hit count at or below which the
// typo-tolerant fallback fires. Mirrors the TS reference: a typo-recovery
// pass only earns its keep when literal matching comes up nearly empty.
const fuzzyFallbackThreshold = 1

// autocompleteFuzzyThreshold is the edit-distance ceiling for the
// autocomplete fuzzy band, by query-unit length. One wider than the
// FuzzyMatchAlgorithm single-result band in the medium range: autocomplete
// shows a RANKED list with fuzzy hits in the strict lowest bucket, so a
// slightly looser net is safe and catches double-edit transpositions the
// match path rejects ("chrons" → "crohns" is OSA distance 2).
func autocompleteFuzzyThreshold(unitLength int) int {
	if unitLength < fuzzyShortLen {
		return 1
	}
	return maxEditDistance
}

// collectFuzzyAutocomplete recovers typo'd queries. For each kind-filtered
// candidate not already placed by the literal prefilter, the query matches
// when ANY candidate token clears the bar against the corresponding query
// unit — Damerau-OSA within the length band OR Double-Metaphone equality.
// Per-token matching keeps a short query ("chrons") from being swamped by the
// edit distance of a long name ("CROHNSDISEASE"). Reuses the same primitives
// as FuzzyMatchAlgorithm for cross-language parity.
func collectFuzzyAutocomplete(queryKey string, wordsInInput []string, candidates, alreadyMatched []CandidateConcept) []CandidateConcept {
	if len(queryKey) == 0 {
		return nil
	}
	matched := make(map[string]struct{}, len(alreadyMatched))
	for _, c := range alreadyMatched {
		matched[autocompleteScoreKey(c)] = struct{}{}
	}
	queryUnits := wordsInInput
	if len(wordsInInput) <= 1 {
		queryUnits = []string{queryKey}
	}
	queryCodes := make([]string, len(queryUnits))
	for i, u := range queryUnits {
		queryCodes[i] = doubleMetaphone(u).primary
	}
	out := make([]CandidateConcept, 0)
	for _, c := range candidates {
		if _, ok := matched[autocompleteScoreKey(c)]; ok {
			continue
		}
		if fuzzyMatchesAnyToken(queryUnits, queryCodes, tokenizeAutocomplete(c.Name)) {
			out = append(out, c)
		}
	}
	return out
}

func fuzzyMatchesAnyToken(queryUnits, queryCodes, candidateTokens []string) bool {
	for u, unit := range queryUnits {
		if len(unit) == 0 {
			continue
		}
		threshold := autocompleteFuzzyThreshold(len(unit))
		code := queryCodes[u]
		for _, token := range candidateTokens {
			if len(token) == 0 {
				continue
			}
			if optimalStringAlignmentDistance(unit, token, threshold) <= threshold {
				return true
			}
			if len(code) > 0 && doubleMetaphone(token).primary == code {
				return true
			}
		}
	}
	return false
}
