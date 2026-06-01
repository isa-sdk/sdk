package reference_test

import (
	"fmt"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// ExampleNewIndex shows the end-to-end shape: adapt a server-side v3
// datasets envelope into a reference.DatasetsResponse, build the Index,
// then Match-and-walk.
func ExampleNewIndex() {
	idx := reference.NewIndex(&reference.DatasetsResponse{
		Version:     "2026.05.29",
		Medications: []reference.Entity{{ID: "LOSARTAN", Name: "Losartan"}},
		Conditions:  []reference.Entity{{ID: "HIGHBLOODPRESSURE", Name: "High Blood Pressure"}},
		ConditionRelations: []reference.Relation{
			{FromID: "HIGHBLOODPRESSURE", ToID: "LOSARTAN", ToName: "Losartan", PrescriptionCount: 4120},
		},
		MedicationRelations: []reference.Relation{
			{FromID: "LOSARTAN", ToID: "HIGHBLOODPRESSURE", ToName: "High Blood Pressure", PrescriptionCount: 4120},
		},
	})

	hbp := idx.Conditions.Match("HIGH BLOOD PRESSURE")
	fmt.Println(hbp.IsKnown(), hbp.Name())
	for _, m := range hbp.Medications(reference.SortMostCommonFirst) {
		fmt.Println(m.Name())
	}
	// Output:
	// true High Blood Pressure
	// Losartan
}

// ExampleIndex_Version shows the staleness check: persist the version
// you built the index from and compare on the next pull. Mismatch =
// rebuild via NewIndex; never mutate an existing Index.
func ExampleIndex_Version() {
	idx := reference.NewIndex(&reference.DatasetsResponse{Version: "2026.05.29"})
	fmt.Println(idx.Version())
	// Output:
	// 2026.05.29
}
