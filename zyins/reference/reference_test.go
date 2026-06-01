package reference

import (
	"testing"
)

// fixtureDatasets returns a small but realistic catalog where:
//   - HBP has three known medications (LISINOPRIL, AMLODIPINE, LOSARTAN)
//     with descending prescription frequency in that order.
//   - INSULIN is a known medication for DIABETES.
//   - alphabetical-vs-frequency divergence is non-trivial.
func fixtureDatasets() *DatasetsResponse {
	return &DatasetsResponse{
		Version: "2026-05-01",
		Medications: []Entity{
			{ID: "LISINOPRIL", Name: "Lisinopril"},
			{ID: "AMLODIPINE", Name: "Amlodipine"},
			{ID: "LOSARTAN", Name: "Losartan"},
			{ID: "INSULIN", Name: "Insulin"},
		},
		Conditions: []Entity{
			{ID: "HIGHBLOODPRESSURE", Name: "High Blood Pressure"},
			{ID: "DIABETES", Name: "Diabetes"},
		},
		ConditionRelations: []Relation{
			{FromID: "HIGHBLOODPRESSURE", ToID: "LISINOPRIL", PrescriptionCount: 100},
			{FromID: "HIGHBLOODPRESSURE", ToID: "AMLODIPINE", PrescriptionCount: 60},
			{FromID: "HIGHBLOODPRESSURE", ToID: "LOSARTAN", PrescriptionCount: 30},
			{FromID: "DIABETES", ToID: "INSULIN", PrescriptionCount: 80},
		},
	}
}

// HBP has only an alias key the consumer typically types. The catalog
// resolves the key via makeKey ("HBP" → "HBP"); for the alias scenario
// we instead add a catalog entity with id == "HBP" and verify that the
// canonical id-keyed maps still resolve.
func fixtureWithHBPAlias() *DatasetsResponse {
	d := fixtureDatasets()
	d.Conditions = append(d.Conditions, Entity{ID: "HBP", Name: "HBP"})
	d.ConditionRelations = append(d.ConditionRelations,
		Relation{FromID: "HBP", ToID: "LISINOPRIL", PrescriptionCount: 100},
		Relation{FromID: "HBP", ToID: "AMLODIPINE", PrescriptionCount: 60},
		Relation{FromID: "HBP", ToID: "LOSARTAN", PrescriptionCount: 30},
	)
	return d
}

func TestMakeKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"High Blood Pressure", "HIGHBLOODPRESSURE"},
		{"hbp", "HBP"},
		{"  insulin-100  ", "INSULIN100"},
		{"", ""},
		{"!!!", ""},
		{"INSULIN", "INSULIN"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := makeKey(c.in); got != c.want {
				t.Errorf("makeKey(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestMatch_UnknownText_ReturnsUnknownConcept(t *testing.T) {
	idx := NewIndex(fixtureDatasets())

	for _, matcher := range []string{"medications", "conditions", "concepts"} {
		t.Run(matcher, func(t *testing.T) {
			var c Concept
			switch matcher {
			case "medications":
				c = idx.Medications.Match("zzz-not-real")
			case "conditions":
				c = idx.Conditions.Match("zzz-not-real")
			case "concepts":
				c = idx.Concepts.Match("zzz-not-real")
			}
			if c.IsKnown() {
				t.Fatal("expected IsKnown=false for unknown text")
			}
			if c.Kind() != KindUnknown {
				t.Errorf("Kind = %q, want %q", c.Kind(), KindUnknown)
			}
			if c.ID() != "" {
				t.Errorf("ID = %q, want empty", c.ID())
			}
			if c.InputText() != "zzz-not-real" {
				t.Errorf("InputText = %q, want preserved", c.InputText())
			}
			if got := c.Conditions(SortMostCommonFirst); len(got) != 0 {
				t.Errorf("Conditions on unknown = %d, want 0", len(got))
			}
			if got := c.Medications(SortMostCommonFirst); len(got) != 0 {
				t.Errorf("Medications on unknown = %d, want 0", len(got))
			}
		})
	}
}

func TestMatch_ConditionMedicationsOrdering(t *testing.T) {
	idx := NewIndex(fixtureWithHBPAlias())
	c := idx.Conditions.Match("hbp")
	if !c.IsKnown() {
		t.Fatal("expected hbp to resolve")
	}
	if c.Kind() != KindCondition {
		t.Errorf("Kind = %q, want condition", c.Kind())
	}

	wantFreq := []string{"LISINOPRIL", "AMLODIPINE", "LOSARTAN"}
	gotFreq := idsOf(c.Medications(SortMostCommonFirst))
	if !equalStrings(gotFreq, wantFreq) {
		t.Errorf("MostCommonFirst = %v, want %v", gotFreq, wantFreq)
	}

	wantAlpha := []string{"AMLODIPINE", "LISINOPRIL", "LOSARTAN"}
	gotAlpha := idsOf(c.Medications(SortAlphabetical))
	if !equalStrings(gotAlpha, wantAlpha) {
		t.Errorf("Alphabetical = %v, want %v", gotAlpha, wantAlpha)
	}
}

func TestRank_IndependentWordIntersection_RanksAboveWordCountNoTolerance(t *testing.T) {
	// Locked TS/JS bucket priority places independentWordIntersection ABOVE
	// wordCountNoTolerance. For query "art dia": "Cart Dia" tokenizes to
	// {CART, DIA} — DIA exact, ART a substring of CART, but not a superset of
	// the input — so it lands in independentWordIntersection. "Extra Art Dia"
	// is a superset of the input tokens, so it lands in wordCountNoTolerance.
	// The independent match must rank first despite the superset's higher
	// frequency.
	algo := NewDefaultAutocompleteAlgorithm()
	candidates := []CandidateConcept{
		{ID: "INDEP", Name: "Cart Dia", Kind: KindCondition},
		{ID: "SUPERSET", Name: "Extra Art Dia", Kind: KindCondition},
	}
	suggestions := algo.Rank(t.Context(), "art dia", candidates,
		AutocompleteOptions{Limit: 5, Frequencies: map[string]int{"SUPERSET": 1000, "INDEP": 1}})
	got := make([]string, len(suggestions))
	for i, s := range suggestions {
		got[i] = s.Concept.ID
	}
	want := []string{"INDEP", "SUPERSET"}
	if !equalStrings(got, want) {
		t.Errorf("Rank order = %v, want %v", got, want)
	}
}

func TestRank_Alphabetical_IgnoresFrequencyAndFlattensBuckets(t *testing.T) {
	// B2: SortAlphabetical keeps the relevance FILTER (only matching
	// candidates survive) but emits a flat case-insensitive A→Z order,
	// frequency-blind, across every bucket. "pressure" matches all three;
	// the high frequency on "High Blood Pressure" must NOT reorder them.
	algo := NewDefaultAutocompleteAlgorithm()
	candidates := []CandidateConcept{
		{ID: "c1", Name: "High Blood Pressure", Kind: KindCondition},
		{ID: "c2", Name: "Low Blood Pressure", Kind: KindCondition},
		{ID: "c3", Name: "Blood Pressure Cuff", Kind: KindCondition},
	}
	suggestions := algo.Rank(t.Context(), "pressure", candidates,
		AutocompleteOptions{
			Limit:       5,
			Frequencies: map[string]int{"c1": 9000},
			Sort:        SortAlphabetical,
		})
	got := make([]string, len(suggestions))
	for i, s := range suggestions {
		got[i] = s.Concept.Name
	}
	want := []string{"Blood Pressure Cuff", "High Blood Pressure", "Low Blood Pressure"}
	if !equalStrings(got, want) {
		t.Errorf("Alphabetical Rank order = %v, want %v", got, want)
	}
}

func TestRank_Alphabetical_ScoreIsFrequencyPlusOne(t *testing.T) {
	// Cross-language parity guard. In SortAlphabetical mode every bucket
	// collapses to one group (scale 1), so each suggestion's score is
	// (frequency+1) — NOT the result position. TS/Python/PHP expose this same
	// frequency-derived score; before this guard Go (and C#) emitted a
	// positional score (len-rank) and diverged for consumers comparing score.
	algo := NewDefaultAutocompleteAlgorithm()
	candidates := []CandidateConcept{
		{ID: "c1", Name: "High Blood Pressure", Kind: KindCondition},
		{ID: "c2", Name: "Low Blood Pressure", Kind: KindCondition},
		{ID: "c3", Name: "Blood Pressure Cuff", Kind: KindCondition},
	}
	suggestions := algo.Rank(t.Context(), "pressure", candidates,
		AutocompleteOptions{
			Limit:       5,
			Frequencies: map[string]int{"c1": 9000, "c2": 3},
			Sort:        SortAlphabetical,
		})
	wantScore := map[string]float64{
		"c1": 9001, // 9000 + 1
		"c2": 4,    // 3 + 1
		"c3": 1,    // 0 + 1 (no frequency entry)
	}
	for _, s := range suggestions {
		if got := s.Score; got != wantScore[s.Concept.ID] {
			t.Errorf("alphabetical score for %s = %v, want %v (frequency+1)", s.Concept.ID, got, wantScore[s.Concept.ID])
		}
	}
}

func TestRank_DefaultSortKeepsFrequencyOrder(t *testing.T) {
	// The zero-value Sort (MostCommonFirst) keeps frequency order — proves
	// Alphabetical is opt-in and the default is byte-identical to before.
	algo := NewDefaultAutocompleteAlgorithm()
	candidates := []CandidateConcept{
		{ID: "c1", Name: "High Blood Pressure", Kind: KindCondition},
		{ID: "c2", Name: "High Cholesterol", Kind: KindCondition},
	}
	suggestions := algo.Rank(t.Context(), "high", candidates,
		AutocompleteOptions{Limit: 5, Frequencies: map[string]int{"c2": 9000, "c1": 1}})
	got := make([]string, len(suggestions))
	for i, s := range suggestions {
		got[i] = s.Concept.ID
	}
	want := []string{"c2", "c1"}
	if !equalStrings(got, want) {
		t.Errorf("default Rank order = %v, want %v", got, want)
	}
}

func TestEquals_CaseInsensitiveAcrossMatchers(t *testing.T) {
	idx := NewIndex(fixtureDatasets())
	upper := idx.Medications.Match("INSULIN")
	lower := idx.Medications.Match("insulin")
	if !upper.Equals(lower) {
		t.Errorf("expected upper.Equals(lower) = true; got false")
	}
	if !lower.Equals(upper) {
		t.Errorf("expected lower.Equals(upper) = true; got false")
	}
}

func TestEquals_DifferentKinds_NotEqual(t *testing.T) {
	idx := NewIndex(fixtureDatasets())
	med := idx.Medications.Match("INSULIN")
	cond := idx.Conditions.Match("DIABETES")
	if med.Equals(cond) {
		t.Error("expected medication.Equals(condition) = false")
	}
}

func TestEquals_UnknownConcepts_MatchOnNormalizedInput(t *testing.T) {
	idx := NewIndex(fixtureDatasets())
	a := idx.Concepts.Match("Made Up Term")
	b := idx.Concepts.Match("made-up-term")
	if !a.Equals(b) {
		t.Error("expected unknown concepts with same normalized input to be equal")
	}
	c := idx.Concepts.Match("Different")
	if a.Equals(c) {
		t.Error("expected unknown concepts with different inputs to be unequal")
	}
}

func TestList_ReturnsCatalogOrder(t *testing.T) {
	idx := NewIndex(fixtureDatasets())

	meds := idx.Medications.List()
	wantMeds := []string{"LISINOPRIL", "AMLODIPINE", "LOSARTAN", "INSULIN"}
	if got := idsOfMed(meds); !equalStrings(got, wantMeds) {
		t.Errorf("Medications.List = %v, want %v", got, wantMeds)
	}

	conds := idx.Conditions.List()
	wantConds := []string{"HIGHBLOODPRESSURE", "DIABETES"}
	if got := idsOfCond(conds); !equalStrings(got, wantConds) {
		t.Errorf("Conditions.List = %v, want %v", got, wantConds)
	}
}

func TestMatchMany_PreservesInputOrder(t *testing.T) {
	idx := NewIndex(fixtureDatasets())
	got := idx.Concepts.MatchMany([]string{"INSULIN", "made-up", "DIABETES"})
	if len(got) != 3 {
		t.Fatalf("MatchMany len = %d, want 3", len(got))
	}
	if got[0].Kind() != KindMedication || got[0].ID() != "INSULIN" {
		t.Errorf("[0] = %+v, want INSULIN medication", got[0])
	}
	if got[1].Kind() != KindUnknown {
		t.Errorf("[1] kind = %q, want unknown", got[1].Kind())
	}
	if got[2].Kind() != KindCondition || got[2].ID() != "DIABETES" {
		t.Errorf("[2] = %+v, want DIABETES condition", got[2])
	}
}

func TestIndexVersion_IsRecorded(t *testing.T) {
	idx := NewIndex(fixtureDatasets())
	if idx.Version() != "2026-05-01" {
		t.Errorf("Version = %q, want 2026-05-01", idx.Version())
	}
}

func TestDatasetVersionChange_InvalidatesIndex(t *testing.T) {
	d1 := fixtureDatasets()
	d1.Version = "v1"
	idx1 := NewIndex(d1)

	d2 := fixtureDatasets()
	d2.Version = "v2"
	// Remove insulin from v2's catalog: a "known" id under v1 must now
	// be unknown under v2.
	d2.Medications = []Entity{
		{ID: "LISINOPRIL", Name: "Lisinopril"},
	}
	d2.ConditionRelations = nil
	d2.MedicationRelations = nil
	idx2 := NewIndex(d2)

	if idx1.Version() == idx2.Version() {
		t.Fatal("test fixture broken: versions must differ")
	}
	if !idx1.Medications.Match("INSULIN").IsKnown() {
		t.Error("INSULIN should be known in v1 index")
	}
	if idx2.Medications.Match("INSULIN").IsKnown() {
		t.Error("INSULIN should NOT be known in v2 index after rebuild")
	}
}

func TestNewIndex_NilSafe(t *testing.T) {
	idx := NewIndex(nil)
	if idx == nil {
		t.Fatal("NewIndex(nil) returned nil")
	}
	if c := idx.Concepts.Match("anything"); c.IsKnown() {
		t.Errorf("expected empty index to return unknown handles; got known")
	}
	if got := idx.Medications.List(); len(got) != 0 {
		t.Errorf("empty index medications list = %d, want 0", len(got))
	}
	if got := idx.Conditions.List(); len(got) != 0 {
		t.Errorf("empty index conditions list = %d, want 0", len(got))
	}
}

func TestMedicationConditionsAccessor(t *testing.T) {
	// LISINOPRIL is prescribed only for HBP/HIGHBLOODPRESSURE in our fixture.
	idx := NewIndex(fixtureWithHBPAlias())
	med := idx.Medications.Match("LISINOPRIL")
	if !med.IsKnown() {
		t.Fatal("LISINOPRIL should be known")
	}
	conds := med.Conditions(SortMostCommonFirst)
	if len(conds) == 0 {
		t.Fatal("expected non-empty conditions for LISINOPRIL")
	}
	// On a medication handle, Medications() returns empty.
	if got := med.Medications(SortMostCommonFirst); len(got) != 0 {
		t.Errorf("Medications() on medication handle = %d, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func idsOf(meds []MedicationConcept) []string {
	out := make([]string, len(meds))
	for i, m := range meds {
		out[i] = m.ID()
	}
	return out
}

func idsOfMed(meds []MedicationConcept) []string { return idsOf(meds) }

func idsOfCond(conds []ConditionConcept) []string {
	out := make([]string, len(conds))
	for i, c := range conds {
		out[i] = c.ID()
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
