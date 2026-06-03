package reference_test

import (
	"fmt"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// ExampleNewFuzzyMatchAlgorithm shows the opt-in typo-tolerant matcher
// recovering a misspelling the default exact matcher cannot: a single
// dropped letter resolves through the Damerau-OSA tier.
func ExampleNewFuzzyMatchAlgorithm() {
	algo := reference.NewFuzzyMatchAlgorithm()
	candidates := []reference.CandidateConcept{
		{ID: "SERTRALINE", Name: "Sertraline", Kind: reference.KindMedication},
		{ID: "TYLENOL", Name: "Tylenol", Kind: reference.KindMedication},
	}
	result := algo.Match("sertaline", candidates)
	fmt.Println(result.Found, result.Candidate.ID)
	// Output:
	// true SERTRALINE
}

// ExampleFuzzyMatchAlgorithm_Match_phonetic shows the phonetic tier
// recovering a homophone misspelling (tylonol → Tylenol) that lies outside
// the edit-distance band — both encode the same Double Metaphone code.
func ExampleFuzzyMatchAlgorithm_Match_phonetic() {
	algo := reference.NewFuzzyMatchAlgorithm()
	candidates := []reference.CandidateConcept{
		{ID: "TYLENOL", Name: "Tylenol", Kind: reference.KindMedication},
	}
	result := algo.Match("tylonol", candidates)
	fmt.Println(result.Found, result.Candidate.ID)
	// Output:
	// true TYLENOL
}

// ExampleWithFuzzyFrequencies shows the intra-tier frequency tie-break:
// two equidistant prefix candidates, the more popular id wins.
func ExampleWithFuzzyFrequencies() {
	algo := reference.NewFuzzyMatchAlgorithm(
		reference.WithFuzzyFrequencies(map[string]int{
			"ASPIRIN_A": 10,
			"ASPIRIN_B": 9000,
		}),
	)
	candidates := []reference.CandidateConcept{
		{ID: "ASPIRIN_A", Name: "Aspirin Brand A", Kind: reference.KindMedication},
		{ID: "ASPIRIN_B", Name: "Aspirin Brand B", Kind: reference.KindMedication},
	}
	// "aspirin brand" (key ASPIRINBRAND) is a prefix of both candidate keys
	// at equal distance; the higher-frequency id wins the tie.
	result := algo.Match("aspirin brand", candidates)
	fmt.Println(result.Candidate.ID)
	// Output:
	// ASPIRIN_B
}
