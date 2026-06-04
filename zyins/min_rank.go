package zyins

// MinRank is the minimum guaranteed-issue rank accepted by the server's
// min_rank filter on prequalify and quote.
//
// The canonical values are MinRankImmediate, MinRankGraded, MinRankRop, and
// MinRankGuaranteed; MinRankReturnOfPremium, MinRankGuaranteedIssue, and
// MinRankGi are synonyms that share the canonical lowercase wire token. The
// server compares case-insensitively and also tolerates numeric strings, so
// option fields stay typed as string rather than MinRank — these constants are
// for ergonomics and autocomplete, not a hard gate. MinRank is itself a string
// type, so every constant assigns to a string field without conversion.
type MinRank string

// MinRank values accepted by the server's min_rank filter.
const (
	// MinRankImmediate is the immediate (full) benefit rank.
	MinRankImmediate MinRank = "immediate"

	// MinRankGraded is the graded-benefit rank.
	MinRankGraded MinRank = "graded"

	// MinRankRop is the return-of-premium rank. Canonical value; see
	// MinRankReturnOfPremium for the synonym.
	MinRankRop MinRank = "rop"

	// MinRankGuaranteed is the guaranteed-issue rank. Canonical value; see
	// MinRankGuaranteedIssue and MinRankGi for synonyms.
	MinRankGuaranteed MinRank = "guaranteed"

	// MinRankReturnOfPremium is a synonym for MinRankRop.
	MinRankReturnOfPremium = MinRankRop

	// MinRankGuaranteedIssue is a synonym for MinRankGuaranteed.
	MinRankGuaranteedIssue = MinRankGuaranteed

	// MinRankGi is a synonym for MinRankGuaranteed.
	MinRankGi = MinRankGuaranteed
)
