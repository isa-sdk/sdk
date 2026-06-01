package zyins

// Cross-language parse-parity conformance for the v3 datasets product-slice
// fields. Loads `shared/schemas/sdk/testdata/datasets_v3_parse_conformance.json`
// — the same corpus the TS / Python / PHP / C# SDKs consume — and asserts the
// Go parser produces the expected canonical output for every scenario:
// empty-vs-absent collapse to a present empty collection, the non-empty-id
// keep predicate with a blank-name default, the non-array-family skip, and the
// int64 epoch bound. Drift between languages surfaces as a failing assertion
// here AND in the sibling test of every other SDK.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

var datasetsV3ParseConformancePath = filepath.Join(
	"..", "..", "..", "shared", "schemas", "sdk", "testdata",
	"datasets_v3_parse_conformance.json",
)

type datasetsV3ConformanceExpectedEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type datasetsV3ConformanceExpected struct {
	Version              string                                           `json:"version"`
	ProductsByFamily     map[string][]datasetsV3ConformanceExpectedEntity `json:"products_by_family"`
	DiscontinuedProducts map[string]int64                                 `json:"discontinued_products"`
	StateDerivatives     []string                                         `json:"state_derivatives"`
}

type datasetsV3ConformanceScenario struct {
	Name         string                        `json:"name"`
	ResponseBody json.RawMessage               `json:"response_body"`
	Expected     datasetsV3ConformanceExpected `json:"expected"`
}

type datasetsV3ConformanceCorpus struct {
	Scenarios []datasetsV3ConformanceScenario `json:"scenarios"`
}

func loadDatasetsV3ConformanceCorpus(t *testing.T) datasetsV3ConformanceCorpus {
	t.Helper()
	raw, err := os.ReadFile(datasetsV3ParseConformancePath)
	if err != nil {
		t.Fatalf("read conformance corpus %s: %v", datasetsV3ParseConformancePath, err)
	}
	var corpus datasetsV3ConformanceCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("decode conformance corpus: %v", err)
	}
	if len(corpus.Scenarios) == 0 {
		t.Fatal("conformance corpus has no scenarios")
	}
	return corpus
}

func TestDatasetsV3Parse_ConformanceCorpus(t *testing.T) {
	corpus := loadDatasetsV3ConformanceCorpus(t)
	for _, scenario := range corpus.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			bundle, err := decodeDatasetsV3Envelope(scenario.ResponseBody)
			if err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if bundle.Version != scenario.Expected.Version {
				t.Errorf("version = %q, want %q", bundle.Version, scenario.Expected.Version)
			}
			assertProductsByFamilyMatch(t, bundle.ProductsByFamily, scenario.Expected.ProductsByFamily)
			assertDiscontinuedMatch(t, bundle.DiscontinuedProducts, scenario.Expected.DiscontinuedProducts)
			assertStateDerivativesMatch(t, bundle.StateDerivatives, scenario.Expected.StateDerivatives)
		})
	}
}

func assertProductsByFamilyMatch(t *testing.T, got map[string][]ReferenceEntity, want map[string][]datasetsV3ConformanceExpectedEntity) {
	t.Helper()
	if got == nil {
		t.Fatal("ProductsByFamily = nil, want non-nil (present-empty contract)")
	}
	if len(got) != len(want) {
		t.Fatalf("ProductsByFamily families = %v, want %v", sortedKeys(got), expectedKeys(want))
	}
	for family, wantRows := range want {
		gotRows, ok := got[family]
		if !ok {
			t.Errorf("ProductsByFamily missing family %q", family)
			continue
		}
		if len(gotRows) != len(wantRows) {
			t.Errorf("family %q rows = %+v, want %+v", family, gotRows, wantRows)
			continue
		}
		for i, wantRow := range wantRows {
			if gotRows[i].ID != wantRow.ID || gotRows[i].Name != wantRow.Name {
				t.Errorf("family %q row %d = %+v, want %+v", family, i, gotRows[i], wantRow)
			}
		}
	}
}

func assertDiscontinuedMatch(t *testing.T, got, want map[string]int64) {
	t.Helper()
	if got == nil {
		t.Fatal("DiscontinuedProducts = nil, want non-nil (present-empty contract)")
	}
	if len(got) != len(want) {
		t.Fatalf("DiscontinuedProducts = %v, want %v", got, want)
	}
	for slug, wantEpoch := range want {
		if got[slug] != wantEpoch {
			t.Errorf("DiscontinuedProducts[%q] = %d, want %d", slug, got[slug], wantEpoch)
		}
	}
}

func assertStateDerivativesMatch(t *testing.T, got, want []string) {
	t.Helper()
	if got == nil {
		t.Fatal("StateDerivatives = nil, want non-nil (present-empty contract)")
	}
	if len(got) != len(want) {
		t.Fatalf("StateDerivatives = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("StateDerivatives[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func sortedKeys(m map[string][]ReferenceEntity) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func expectedKeys(m map[string][]datasetsV3ConformanceExpectedEntity) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
