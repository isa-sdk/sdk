package zyins

// Cross-SDK V3 decoded-response equivalence — Go SDK in-process gate.
//
// Loads the shared fixture (conformance/scenarios/.fixtures/prequalify-v3-fex-immediate.response.json)
// and the shared expected triples (…expected.json), decodes the fixture
// through decodePrequalifyV3Envelope (the same function PrequalifyV3Service.Run
// calls after receiving the HTTP body), and asserts the decoded values match
// the shared expected triples.
//
// Because this test lives in package zyins it has direct access to the
// unexported decodePrequalifyV3Envelope — no public shim required.
//
// Sibling tests in packages/python, packages/csharp, and packages/php assert
// against the same expected.json so any decode divergence between languages
// produces a failing test in the diverging language's own CI job.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// expectedTriple is the per-plan decoded value the shared expected.json records.
type expectedTriple struct {
	ID                  string `json:"id"`
	PremiumCents        int64  `json:"premium_cents"`
	EligibilityCategory string `json:"eligibility_category"`
}

type crossSDKExpected struct {
	Plans []expectedTriple `json:"plans"`
}

func TestCrossSDKEquivalence_PrequalifyV3(t *testing.T) {
	fixtureBody := readCrossSDKFixture(t, "prequalify-v3-fex-immediate.response.json")
	expected := readCrossSDKExpected(t)

	result, err := decodePrequalifyV3Envelope(fixtureBody, "conformance-fixture-key")
	if err != nil {
		t.Fatalf("decodePrequalifyV3Envelope: %v", err)
	}

	if len(result.Plans) < len(expected.Plans) {
		t.Fatalf("decoded %d plans, want at least %d", len(result.Plans), len(expected.Plans))
	}

	for i, want := range expected.Plans {
		offer := result.Plans[i]

		if offer.ID != want.ID {
			t.Errorf("plan[%d].id = %q, want %q", i, offer.ID, want.ID)
		}

		prem := OfferPremium(offer)
		if prem == nil {
			t.Errorf("plan[%d] OfferPremium = nil, want %d cents", i, want.PremiumCents)
			continue
		}
		if prem.Amount.Cents != want.PremiumCents {
			t.Errorf("plan[%d].premium_cents = %d, want %d", i, prem.Amount.Cents, want.PremiumCents)
		}

		var gotCategory string
		for _, row := range offer.Pricing {
			if row.Primary && row.Eligibility.Category != nil {
				gotCategory = string(*row.Eligibility.Category)
				break
			}
		}
		if gotCategory != want.EligibilityCategory {
			t.Errorf("plan[%d].eligibility_category = %q, want %q", i, gotCategory, want.EligibilityCategory)
		}
	}
}

// conformanceFixturesDir locates the conformance/scenarios/.fixtures directory
// by walking up from the test file's location.
func conformanceFixturesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// Walk from packages/go/zyins up to the repo root.
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "conformance", "scenarios", ".fixtures")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("conformance/scenarios/.fixtures not found by walking up from test file")
	return ""
}

func readCrossSDKFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(conformanceFixturesDir(t), name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return data
}

func readCrossSDKExpected(t *testing.T) crossSDKExpected {
	t.Helper()
	path := filepath.Join(conformanceFixturesDir(t), "prequalify-v3-fex-immediate.expected.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read expected %s: %v", path, err)
	}
	var out crossSDKExpected
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	if len(out.Plans) == 0 {
		t.Fatal(fmt.Sprintf("expected.json has no plans at %s", path))
	}
	return out
}
