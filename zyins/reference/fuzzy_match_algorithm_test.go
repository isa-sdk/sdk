package reference

import "testing"

// cond builds a minimal condition candidate for the matcher tests,
// mirroring the TS fixture's condition() helper.
func cond(id, name string) CandidateConcept {
	return CandidateConcept{ID: id, Name: name, Kind: KindCondition}
}

var (
	crohns     = cond("CROHNS", "Crohn's Disease")
	sertraline = cond("SERTRALINE", "Sertraline")
	tylenol    = cond("TYLENOL", "Tylenol")
	lisinopril = cond("LISINOPRIL", "Lisinopril")
	losartan   = cond("LOSARTAN", "Losartan")
	metformin  = cond("METFORMIN", "Metformin")
)

func fuzzyCatalog() []CandidateConcept {
	return []CandidateConcept{crohns, sertraline, tylenol, lisinopril, losartan, metformin}
}

func TestFuzzyMatch_Gibberish_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	m := NewFuzzyMatchAlgorithm()
	if got := m.Match("zzqqxxjjww", fuzzyCatalog()); got.Found {
		t.Errorf("Match(gibberish) = %+v, want Found=false", got)
	}
}

func TestFuzzyMatch_SymbolOnlyInput_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	m := NewFuzzyMatchAlgorithm()
	if got := m.Match("!!!", fuzzyCatalog()); got.Found {
		t.Errorf("Match(symbols) = %+v, want Found=false", got)
	}
}

func TestFuzzyMatch_ExactTier_ResolvesNameAndID(t *testing.T) {
	t.Parallel()
	m := NewFuzzyMatchAlgorithm()
	tests := []struct {
		name, query, wantID string
	}{
		{"exact name", "Lisinopril", "LISINOPRIL"},
		{"exact id", "LISINOPRIL", "LISINOPRIL"},
		{"case and punctuation insensitive", "crohn's disease", "CROHNS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := m.Match(tt.query, fuzzyCatalog())
			if !got.Found || got.Candidate.ID != tt.wantID {
				t.Errorf("Match(%q) = %+v, want id %q", tt.query, got, tt.wantID)
			}
		})
	}
}

func TestFuzzyMatch_DamerauTier_RecoversTransposition(t *testing.T) {
	t.Parallel()
	if d := optimalStringAlignmentDistance("CROHNS", "CORHNS", maxEditDistance); d != 1 {
		t.Fatalf("OSA(CROHNS, CORHNS) = %d, want 1", d)
	}
	m := NewFuzzyMatchAlgorithm()
	got := m.Match("corhn's disease", fuzzyCatalog())
	if !got.Found || got.Candidate.ID != "CROHNS" {
		t.Errorf("Match(corhn's disease) = %+v, want CROHNS", got)
	}
}

func TestFuzzyMatch_DamerauTier_RecoversDroppedLetter(t *testing.T) {
	t.Parallel()
	if d := optimalStringAlignmentDistance("SERTALINE", "SERTRALINE", maxEditDistance); d != 1 {
		t.Fatalf("OSA(SERTALINE, SERTRALINE) = %d, want 1", d)
	}
	m := NewFuzzyMatchAlgorithm()
	got := m.Match("sertaline", fuzzyCatalog())
	if !got.Found || got.Candidate.ID != "SERTRALINE" {
		t.Errorf("Match(sertaline) = %+v, want SERTRALINE", got)
	}
}

func TestFuzzyMatch_Band_LongQueryAdmitsTwoEditsRejectsUnreachable(t *testing.T) {
	t.Parallel()
	long := cond("HYDROCHLOROTHIAZIDE", "Hydrochlorothiazide")
	pool := []CandidateConcept{long}
	m := NewFuzzyMatchAlgorithm()
	if got := m.Match("hydrochlorothiazode", pool); !got.Found || got.Candidate.ID != "HYDROCHLOROTHIAZIDE" {
		t.Errorf("Match(hydrochlorothiazode) = %+v, want HYDROCHLOROTHIAZIDE", got)
	}
	if got := m.Match("zzzzzzzzzzzzzzz", pool); got.Found {
		t.Errorf("Match(unreachable) = %+v, want Found=false", got)
	}
}

func TestFuzzyMatch_PhoneticTier_RecoversVowelSwap(t *testing.T) {
	t.Parallel()
	if doubleMetaphone("tylonol").primary != doubleMetaphone("tylenol").primary {
		t.Fatal("tylonol and tylenol should share a metaphone primary code")
	}
	m := NewFuzzyMatchAlgorithm()
	got := m.Match("tylonol", fuzzyCatalog())
	if !got.Found || got.Candidate.ID != "TYLENOL" {
		t.Errorf("Match(tylonol) = %+v, want TYLENOL", got)
	}
}

func TestFuzzyMatch_PhoneticTier_RecoversBeyondEditBand(t *testing.T) {
	t.Parallel()
	// METPHORMIN vs METFORMIN is OSA distance 2, outside the band for a
	// 10-char query (threshold 1); PH/F homophony lets the phonetic tier win.
	if d := optimalStringAlignmentDistance("METPHORMIN", "METFORMIN", maxEditDistance); d != 2 {
		t.Fatalf("OSA(METPHORMIN, METFORMIN) = %d, want 2", d)
	}
	if th := fuzzyThresholdForLength(len("METPHORMIN")); th != 1 {
		t.Fatalf("threshold(10) = %d, want 1", th)
	}
	if doubleMetaphone("metphormin").primary != doubleMetaphone("metformin").primary {
		t.Fatal("metphormin and metformin should share a metaphone primary code")
	}
	m := NewFuzzyMatchAlgorithm()
	got := m.Match("metphormin", fuzzyCatalog())
	if !got.Found || got.Candidate.ID != "METFORMIN" {
		t.Errorf("Match(metphormin) = %+v, want METFORMIN", got)
	}
}

func TestFuzzyMatch_TierOrdering_ExactBeatsPrefix(t *testing.T) {
	t.Parallel()
	lis := cond("LISINOPRIL", "Lisinopril")
	lisHctz := cond("LISINOPRILHCTZ", "Lisinopril HCTZ")
	m := NewFuzzyMatchAlgorithm()
	got := m.Match("Lisinopril", []CandidateConcept{lis, lisHctz})
	if !got.Found || got.Candidate.ID != "LISINOPRIL" {
		t.Errorf("Match(Lisinopril) = %+v, want LISINOPRIL", got)
	}
}

func TestFuzzyMatch_TierOrdering_PrefixBeatsDamerau(t *testing.T) {
	t.Parallel()
	meto := cond("METOPROLOL", "Metoprolol")
	metf := cond("METFORMIN", "Metformin")
	m := NewFuzzyMatchAlgorithm()
	got := m.Match("meto", []CandidateConcept{metf, meto})
	if !got.Found || got.Candidate.ID != "METOPROLOL" {
		t.Errorf("Match(meto) = %+v, want METOPROLOL", got)
	}
}

func TestFuzzyMatch_FrequencyTieBreak_HigherFrequencyWins(t *testing.T) {
	t.Parallel()
	a := cond("CONDA", "Conda")
	b := cond("CONDB", "Condb")
	m := NewFuzzyMatchAlgorithm(WithFuzzyFrequencies(map[string]int{
		"CONDA": 10,
		"CONDB": 9000,
	}))
	got := m.Match("cond", []CandidateConcept{a, b})
	if !got.Found || got.Candidate.ID != "CONDB" {
		t.Errorf("Match(cond) = %+v, want CONDB (higher frequency)", got)
	}
}

func TestFuzzyMatch_TieBreak_DeterministicNameOrderWithoutFrequencies(t *testing.T) {
	t.Parallel()
	a := cond("CONDB", "Condb")
	b := cond("CONDA", "Conda")
	m := NewFuzzyMatchAlgorithm()
	// Equal-distance prefix tie breaks by normalized name: conda < condb, so
	// CONDA wins regardless of candidate order.
	if got := m.Match("cond", []CandidateConcept{a, b}); got.Candidate.ID != "CONDA" {
		t.Errorf("Match(cond) order1 = %+v, want CONDA", got)
	}
	if got := m.Match("cond", []CandidateConcept{b, a}); got.Candidate.ID != "CONDA" {
		t.Errorf("Match(cond) order2 = %+v, want CONDA", got)
	}
}

func TestFuzzyMatch_NFCNormalization_PrecomposedMatchesDecomposed(t *testing.T) {
	t.Parallel()
	precomposed := "café" // café (single code point)
	decomposed := "café" // café (e + combining acute)
	if precomposed == decomposed {
		t.Fatal("test setup error: spellings must differ before NFC")
	}
	cafe := cond("CAFE", decomposed)
	m := NewFuzzyMatchAlgorithm()
	got := m.Match(precomposed, []CandidateConcept{cafe})
	if !got.Found || got.Candidate.ID != "CAFE" {
		t.Errorf("Match(precomposed café) = %+v, want CAFE", got)
	}
}

func TestFuzzyThresholdForLength_ElasticsearchBand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		length, want int
	}{
		{3, 1}, {5, 1}, {6, 1}, {12, 1}, {13, 2}, {40, 2},
	}
	for _, tt := range tests {
		if got := fuzzyThresholdForLength(tt.length); got != tt.want {
			t.Errorf("fuzzyThresholdForLength(%d) = %d, want %d", tt.length, got, tt.want)
		}
	}
}

func TestOptimalStringAlignmentDistance_OSASemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, a, b string
		maxDist    int
		want       int
	}{
		{"adjacent transposition", "ab", "ba", maxEditDistance, 1},
		{"transposition plus insertion costs 3", "CA", "ABC", maxEditDistance, 3},
		{"insertion", "cat", "cats", maxEditDistance, 1},
		{"deletion", "cats", "cat", maxEditDistance, 1},
		{"substitution", "cat", "cot", maxEditDistance, 1},
		{"length gap short-circuit", "a", "abcdef", maxEditDistance, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := optimalStringAlignmentDistance(tt.a, tt.b, tt.maxDist); got != tt.want {
				t.Errorf("OSA(%q, %q, %d) = %d, want %d", tt.a, tt.b, tt.maxDist, got, tt.want)
			}
		})
	}
}

func TestFuzzyMatch_Clone_OverridesVersionTagPreservesFrequencies(t *testing.T) {
	t.Parallel()
	base := NewFuzzyMatchAlgorithm(
		WithFuzzyVersionTag("base"),
		WithFuzzyFrequencies(map[string]int{"CONDB": 9000}),
	)
	tenant := base.Clone(WithFuzzyVersionTag("tenant"))
	if base.VersionTag() != "base" || tenant.VersionTag() != "tenant" {
		t.Errorf("VersionTag base=%q tenant=%q, want base/tenant",
			base.VersionTag(), tenant.VersionTag())
	}
	// Frequencies survive the clone: CONDB still outranks CONDA on a tie.
	a := cond("CONDA", "Conda")
	b := cond("CONDB", "Condb")
	if got := tenant.Match("cond", []CandidateConcept{a, b}); got.Candidate.ID != "CONDB" {
		t.Errorf("cloned matcher lost frequencies: Match(cond) = %+v, want CONDB", got)
	}
}

// TestFuzzyMatch_SatisfiesMatchAlgorithm is a compile-time assertion that
// FuzzyMatchAlgorithm is a drop-in MatchAlgorithm.
func TestFuzzyMatch_SatisfiesMatchAlgorithm(t *testing.T) {
	t.Parallel()
	var _ MatchAlgorithm = NewFuzzyMatchAlgorithm()
}
