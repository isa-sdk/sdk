package reference

// Optimal String Alignment distance (Damerau-Levenshtein restricted to
// adjacent transpositions). Counts insertions, deletions, substitutions,
// and swaps of two adjacent characters — so chrons → crohns costs 1, not
// 2. Ported from packages/ts/src/zyins/reference/_damerauOsa.ts; the
// length-scaled threshold band and early-exit behavior are identical.

const (
	// fuzzyShortLen is the upper bound (exclusive) for the short-query
	// band: queries below this length tolerate at most one edit.
	fuzzyShortLen = 6
	// fuzzyMediumLen is the upper bound (inclusive) for the medium-query
	// band: queries up to this length tolerate at most one edit.
	fuzzyMediumLen = 12
	// maxEditDistance caps the tolerated edit distance for any query
	// length.
	maxEditDistance = 2
)

// fuzzyThresholdForLength returns the maximum edit distance tolerated for a
// query of the given length. It never exceeds maxEditDistance.
func fuzzyThresholdForLength(queryLength int) int {
	if queryLength < fuzzyShortLen {
		return 1
	}
	if queryLength <= fuzzyMediumLen {
		return 1
	}
	return maxEditDistance
}

// optimalStringAlignmentDistance computes the OSA distance between a and b.
// It returns maxDistance+1 early once every cell in the active row exceeds
// maxDistance, so a far-apart pair costs far less than the full matrix.
//
// a and b are compared as keys (ASCII-only output of makeKey), so byte
// indexing matches the TS charAt over the same key space.
func optimalStringAlignmentDistance(a, b string, maxDistance int) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	if abs(len(a)-len(b)) > maxDistance {
		return maxDistance + 1
	}

	cols := len(b) + 1
	prevPrev := make([]int, cols)
	prev := make([]int, cols)
	curr := make([]int, cols)
	for j := 0; j < cols; j++ {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			value := min3(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
			if i > 1 && j > 1 &&
				a[i-1] == b[j-2] &&
				a[i-2] == b[j-1] {
				if t := prevPrev[j-2] + 1; t < value {
					value = t // transposition
				}
			}
			curr[j] = value
			if value < rowMin {
				rowMin = value
			}
		}
		if rowMin > maxDistance {
			return maxDistance + 1
		}
		prevPrev, prev, curr = prev, curr, prevPrev
	}
	return prev[len(b)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
