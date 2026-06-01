package zyins

// Conformance test for the v3 `reference` namespace.
//
// Loads `shared/schemas/sdk/testdata/reference_vectors.json` — the
// cross-language ground truth — and asserts the Go SDK matches every
// `make_key` parity vector and every `match()` scenario. The same JSON
// drives the TS / Python / C# / PHP parity tests; drift between
// languages must surface here.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// referenceVectorsPath resolves the canonical corpus relative to the
// package directory. The Go test runner's cwd is the package dir at
// test time, so a fixed relative path is stable across machines.
var referenceVectorsPath = filepath.Join(
	"..", "..", "..", "shared", "schemas", "sdk", "testdata", "reference_vectors.json",
)

type refMakeKeyVector struct {
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

type refBundleEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type refBundleFixture struct {
	Version                string              `json:"version"`
	Conditions             []refBundleEntity   `json:"conditions"`
	Medications            []refBundleEntity   `json:"medications"`
	MedicationsByCondition map[string][]string `json:"medications_by_condition"`
	FrequencyGraphs        struct {
		UseMap map[string]map[string]int `json:"use_map"`
	} `json:"frequency_graphs"`
}

type refMatchScenario struct {
	Name                       string   `json:"name"`
	Matcher                    string   `json:"matcher"`
	Input                      string   `json:"input"`
	ExpectedKind               string   `json:"expected_kind"`
	ExpectedKnown              bool     `json:"expected_known"`
	ExpectedID                 *string  `json:"expected_id"`
	InputTextPreserved         *string  `json:"input_text_preserved"`
	MedicationsMostCommonFirst []string `json:"medications_most_common_first"`
	MedicationsAlphabetical    []string `json:"medications_alphabetical"`
	ConditionsMostCommonFirst  []string `json:"conditions_most_common_first"`
	ConditionsAnyKnown         *bool    `json:"conditions_any_known"`
}

type refVectorFile struct {
	MakeKey []refMakeKeyVector `json:"make_key"`
	Bundle  refBundleFixture   `json:"bundle"`
	Matches []refMatchScenario `json:"matches"`
}

func loadReferenceVectors(t *testing.T) *refVectorFile {
	t.Helper()
	raw, err := os.ReadFile(referenceVectorsPath)
	if err != nil {
		t.Fatalf("zyins: failed to read reference vectors at %s: %v", referenceVectorsPath, err)
	}
	var v refVectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("zyins: failed to parse reference vectors: %v", err)
	}
	return &v
}

func bundleFromFixture(f *refBundleFixture) *DatasetBundleV3 {
	conditions := make([]ReferenceEntity, len(f.Conditions))
	for i, c := range f.Conditions {
		conditions[i] = ReferenceEntity{ID: c.ID, Name: c.Name}
	}
	medications := make([]ReferenceEntity, len(f.Medications))
	for i, m := range f.Medications {
		medications[i] = ReferenceEntity{ID: m.ID, Name: m.Name}
	}
	// Translate the legacy fixture maps-shape into the v3 inline-row
	// relation edges. The cross-language conformance corpus still
	// publishes the maps shape; the SDK consumes inline rows on the
	// wire but the per-edge information is equivalent.
	var condEdges []RelationEdge
	for _, cond := range f.Conditions {
		for _, medID := range f.MedicationsByCondition[cond.ID] {
			condEdges = append(condEdges, RelationEdge{
				FromID:            cond.ID,
				ToID:              medID,
				PrescriptionCount: f.FrequencyGraphs.UseMap[cond.ID][medID],
			})
		}
	}
	return &DatasetBundleV3{
		Version:            f.Version,
		Conditions:         conditions,
		Medications:        medications,
		ConditionRelations: condEdges,
		Datasets:           map[DatasetCategory]*DatasetEntry{},
	}
}

// TestReferenceMakeKey_ParityVectors asserts the internal normalizer
// matches every cross-language make_key vector byte-for-byte.
func TestReferenceMakeKey_ParityVectors(t *testing.T) {
	vectors := loadReferenceVectors(t)
	for _, vec := range vectors.MakeKey {
		vec := vec
		t.Run(vec.Input, func(t *testing.T) {
			if got := makeKey(vec.Input); got != vec.Expected {
				t.Errorf("makeKey(%q) = %q, want %q", vec.Input, got, vec.Expected)
			}
		})
	}
}

// TestReferenceMatch_Scenarios walks the cross-language match() corpus.
// Each scenario asserts the returned Concept's kind / known / id /
// input-preservation, and where present, the ordered accessor output.
func TestReferenceMatch_Scenarios(t *testing.T) {
	vectors := loadReferenceVectors(t)
	bundle := bundleFromFixture(&vectors.Bundle)
	svc := newReferenceService()

	for _, scenario := range vectors.Matches {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			var got Concept
			switch scenario.Matcher {
			case "medications":
				got = svc.Medications().Match(scenario.Input, bundle)
			case "conditions":
				got = svc.Conditions().Match(scenario.Input, bundle)
			case "concepts":
				got = svc.Concepts().Match(scenario.Input, bundle)
			default:
				t.Fatalf("unknown matcher %q", scenario.Matcher)
			}

			if string(got.Kind()) != scenario.ExpectedKind {
				t.Errorf("kind = %q, want %q", got.Kind(), scenario.ExpectedKind)
			}
			if got.IsKnown() != scenario.ExpectedKnown {
				t.Errorf("isKnown = %v, want %v", got.IsKnown(), scenario.ExpectedKnown)
			}
			expectedID := ""
			if scenario.ExpectedID != nil {
				expectedID = *scenario.ExpectedID
			}
			if got.ID() != expectedID {
				t.Errorf("id = %q, want %q", got.ID(), expectedID)
			}
			if got.InputText() != scenario.Input {
				t.Errorf("inputText = %q, want %q", got.InputText(), scenario.Input)
			}
			if scenario.InputTextPreserved != nil && got.InputText() != *scenario.InputTextPreserved {
				t.Errorf("inputText (preserved) = %q, want %q", got.InputText(), *scenario.InputTextPreserved)
			}
			if scenario.MedicationsMostCommonFirst != nil {
				assertIDs(t, "medications_most_common_first", got.Medications(SortMostCommonFirst), scenario.MedicationsMostCommonFirst)
			}
			if scenario.MedicationsAlphabetical != nil {
				assertIDs(t, "medications_alphabetical", got.Medications(SortAlphabetical), scenario.MedicationsAlphabetical)
			}
			if scenario.ConditionsMostCommonFirst != nil {
				assertIDs(t, "conditions_most_common_first", got.Conditions(SortMostCommonFirst), scenario.ConditionsMostCommonFirst)
			}
			if scenario.ConditionsAnyKnown != nil && *scenario.ConditionsAnyKnown {
				conds := got.Conditions(SortMostCommonFirst)
				if len(conds) == 0 {
					t.Errorf("conditions_any_known: expected non-empty, got 0")
				}
				for _, c := range conds {
					if !c.IsKnown() {
						t.Errorf("conditions_any_known: found unknown handle id=%q", c.ID())
					}
				}
			}
		})
	}
}

// TestReferenceLiveBug — the canonical regression the v3 namespace
// exists to fix. Mirrors the equivalent TS test.
func TestReferenceLiveBug_HBPMedicationsMostCommonFirst(t *testing.T) {
	vectors := loadReferenceVectors(t)
	bundle := bundleFromFixture(&vectors.Bundle)
	svc := newReferenceService()

	concept := svc.Conditions().Match("hbp", bundle)
	if !concept.IsKnown() {
		t.Fatalf("expected hbp to resolve to a known condition")
	}
	meds := concept.Medications(SortMostCommonFirst)
	want := []string{"LISINOPRIL", "AMLODIPINE", "LOSARTAN"}
	assertIDs(t, "live-bug ordering", meds, want)
}

// TestReferenceUnknownText asserts that unknown text yields an unknown
// handle with empty accessors and preserved input — not an error.
func TestReferenceUnknownText(t *testing.T) {
	vectors := loadReferenceVectors(t)
	bundle := bundleFromFixture(&vectors.Bundle)
	svc := newReferenceService()

	concept := svc.Concepts().Match("unknown free text", bundle)
	if concept.IsKnown() {
		t.Errorf("expected isKnown=false")
	}
	if concept.ID() != "" {
		t.Errorf("expected empty ID, got %q", concept.ID())
	}
	if concept.InputText() != "unknown free text" {
		t.Errorf("inputText not preserved: %q", concept.InputText())
	}
	if len(concept.Medications(SortMostCommonFirst)) != 0 {
		t.Errorf("expected empty medications accessor")
	}
	if len(concept.Conditions(SortMostCommonFirst)) != 0 {
		t.Errorf("expected empty conditions accessor")
	}
}

// TestReferenceRelatedHandlesPreserveInput asserts that walking the
// graph from a matched handle preserves the original input text on
// every related concept — matches the TS contract.
func TestReferenceRelatedHandlesPreserveInput(t *testing.T) {
	vectors := loadReferenceVectors(t)
	bundle := bundleFromFixture(&vectors.Bundle)
	svc := newReferenceService()

	condition := svc.Conditions().Match("hbp", bundle)
	meds := condition.Medications(SortMostCommonFirst)
	if len(meds) == 0 || meds[0].InputText() != "hbp" {
		t.Errorf("related medication input = %q, want %q", inputTextOf(meds, 0), "hbp")
	}

	medication := svc.Medications().Match("lisinopril", bundle)
	conds := medication.Conditions(SortMostCommonFirst)
	if len(conds) == 0 || conds[0].InputText() != "lisinopril" {
		t.Errorf("related condition input = %q, want %q", inputTextOf(conds, 0), "lisinopril")
	}
}

func TestReferenceConditionsForMedication_UsesDatasetConditionOrder(t *testing.T) {
	const medicationID = "MED"
	bundle := &DatasetBundleV3{
		Conditions: []ReferenceEntity{
			{ID: "COND_B", Name: "Beta"},
			{ID: "COND_A", Name: "Alpha"},
		},
		Medications: []ReferenceEntity{{ID: medicationID, Name: "Medication"}},
		// Inline edges are emitted out of Conditions order; the catalog
		// must derive the reverse index in Conditions (display) order
		// — that's what UsesDatasetConditionOrder asserts.
		ConditionRelations: []RelationEdge{
			{FromID: "COND_A", ToID: medicationID, PrescriptionCount: 1},
			{FromID: "COND_B", ToID: medicationID, PrescriptionCount: 1},
		},
	}

	got := newReferenceService().Medications().Match("med", bundle)
	assertIDs(t, "condition tie order", got.Conditions(SortMostCommonFirst), []string{"COND_B", "COND_A"})
}

func inputTextOf(handles []Concept, i int) string {
	if i < 0 || i >= len(handles) {
		return ""
	}
	return handles[i].InputText()
}

func assertIDs(t *testing.T, label string, handles []Concept, want []string) {
	t.Helper()
	got := make([]string, len(handles))
	for i, h := range handles {
		got[i] = h.ID()
	}
	if len(got) != len(want) {
		t.Errorf("%s: len = %d, want %d (got=%v, want=%v)", label, len(got), len(want), got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q (full got=%v, want=%v)", label, i, got[i], want[i], got, want)
		}
	}
}
