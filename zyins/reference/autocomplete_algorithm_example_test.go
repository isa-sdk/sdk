package reference_test

import (
	"context"
	"fmt"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// ExampleNewDefaultAutocompleteAlgorithm shows the locked-spec bucket
// priority: a prefix match ranks above a same-word match, which ranks
// above a tolerant superset match.
func ExampleNewDefaultAutocompleteAlgorithm() {
	algo := reference.NewDefaultAutocompleteAlgorithm()
	candidates := []reference.CandidateConcept{
		{ID: "DIABETESMELLITUS", Name: "Diabetes Mellitus", Kind: reference.KindCondition},
		{ID: "DIABETESINSIPIDUS", Name: "Diabetes Insipidus", Kind: reference.KindCondition},
		{ID: "PREDIABETES", Name: "Prediabetes", Kind: reference.KindCondition},
	}
	suggestions := algo.Rank(context.Background(), "diabetes", candidates,
		reference.AutocompleteOptions{Limit: 3})
	for _, s := range suggestions {
		fmt.Println(s.Concept.Name)
	}
	// Output:
	// Diabetes Mellitus
	// Diabetes Insipidus
	// Prediabetes
}

// ExampleDefaultAutocompleteAlgorithm_Rank_frequencyBoost shows the
// within-bucket boost: when two candidates share a bucket, the one with
// a higher prescription_count entry sorts first.
func ExampleDefaultAutocompleteAlgorithm_Rank_frequencyBoost() {
	algo := reference.NewDefaultAutocompleteAlgorithm()
	candidates := []reference.CandidateConcept{
		{ID: "LOSARTAN", Name: "Losartan", Kind: reference.KindMedication},
		{ID: "LISINOPRIL", Name: "Lisinopril", Kind: reference.KindMedication},
	}
	// Lisinopril is more commonly prescribed; the boost lifts it above
	// alphabetical order even though both share the same bucket.
	suggestions := algo.Rank(context.Background(), "l", candidates,
		reference.AutocompleteOptions{
			Limit:       2,
			Frequencies: map[string]int{"LISINOPRIL": 4120, "LOSARTAN": 880},
		})
	for _, s := range suggestions {
		fmt.Println(s.Concept.Name)
	}
	// Output:
	// Lisinopril
	// Losartan
}

// ExampleDefaultAutocompleteAlgorithm_Rank_kindFilter shows the kind
// filter applied before ranking: medication-typed candidates are dropped
// when only conditions are requested.
func ExampleDefaultAutocompleteAlgorithm_Rank_kindFilter() {
	algo := reference.NewDefaultAutocompleteAlgorithm()
	candidates := []reference.CandidateConcept{
		{ID: "HIGHBLOODPRESSURE", Name: "High Blood Pressure", Kind: reference.KindCondition},
		{ID: "HUMIRA", Name: "Humira", Kind: reference.KindMedication},
	}
	suggestions := algo.Rank(context.Background(), "h", candidates,
		reference.AutocompleteOptions{
			Limit: 5,
			Kinds: []reference.ConceptKind{reference.KindCondition},
		})
	for _, s := range suggestions {
		fmt.Println(s.Concept.Name)
	}
	// Output:
	// High Blood Pressure
}
