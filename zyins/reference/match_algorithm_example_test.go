package reference_test

import (
	"fmt"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// ExampleNewDefaultMatchAlgorithm shows the locked-spec contract:
// MakeKey-normalize the query (uppercase + ASCII-alphanumeric strip),
// then exact-key lookup against each candidate's normalized id or name.
func ExampleNewDefaultMatchAlgorithm() {
	algo := reference.NewDefaultMatchAlgorithm()
	candidates := []reference.CandidateConcept{
		{ID: "HIGHBLOODPRESSURE", Name: "High Blood Pressure", Kind: reference.KindCondition},
		{ID: "DIABETES", Name: "Diabetes", Kind: reference.KindCondition},
	}
	result := algo.Match("high-blood pressure!", candidates)
	fmt.Println(result.Found, result.Candidate.ID, result.Candidate.Name)
	// Output:
	// true HIGHBLOODPRESSURE High Blood Pressure
}

// ExampleDefaultMatchAlgorithm_Match_unknownReturnsFalse shows the
// locked-spec rule: unknown text never errors. The caller branches on
// Found and forwards the verbatim query to prequalify when Found=false.
func ExampleDefaultMatchAlgorithm_Match_unknownReturnsFalse() {
	algo := reference.NewDefaultMatchAlgorithm()
	candidates := []reference.CandidateConcept{
		{ID: "INSULIN", Name: "Insulin", Kind: reference.KindMedication},
	}
	result := algo.Match("NewExperimental XR 2026", candidates)
	fmt.Println(result.Found)
	// Output:
	// false
}

// ExampleDefaultMatchAlgorithm_Clone shows that a default match
// algorithm is cheap to clone with a version-tag override — useful when
// pinning a tenant to a specific catalog version for auditability.
func ExampleDefaultMatchAlgorithm_Clone() {
	base := reference.NewDefaultMatchAlgorithm(
		reference.WithMatchAlgorithmVersionTag("base"))
	tenant := base.Clone(reference.WithMatchAlgorithmVersionTag("tenant-acme"))
	fmt.Println(base.VersionTag(), tenant.VersionTag())
	// Output:
	// base tenant-acme
}
