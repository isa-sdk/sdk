package reference

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// doubleMetaphoneVector mirrors one entry of the cross-language fixture
// doubleMetaphone.vectors.ts, copied verbatim into testdata so the Go port
// asserts against the exact same data the TS / PHP / C# / Python ports use.
type doubleMetaphoneVector struct {
	Term      string `json:"term"`
	Primary   string `json:"primary"`
	Alternate string `json:"alternate"`
}

func loadDoubleMetaphoneVectors(t *testing.T) []doubleMetaphoneVector {
	t.Helper()
	path := filepath.Join("testdata", "double_metaphone_vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read vector fixture %q: %v", path, err)
	}
	var vectors []doubleMetaphoneVector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("failed to parse vector fixture %q: %v", path, err)
	}
	if len(vectors) == 0 {
		t.Fatalf("vector fixture %q is empty", path)
	}
	return vectors
}

// TestDoubleMetaphone_FixtureVectors_MatchTypeScriptCodes asserts the Go
// encoder reproduces the primary AND alternate codes pinned by the shared
// cross-language fixture for every term. Any divergence fails — this is the
// parity contract enforced identically across the language ports.
func TestDoubleMetaphone_FixtureVectors_MatchTypeScriptCodes(t *testing.T) {
	t.Parallel()
	vectors := loadDoubleMetaphoneVectors(t)
	for _, v := range vectors {
		t.Run(v.Term, func(t *testing.T) {
			t.Parallel()
			got := doubleMetaphone(v.Term)
			if got.primary != v.Primary {
				t.Errorf("doubleMetaphone(%q).primary = %q, want %q",
					v.Term, got.primary, v.Primary)
			}
			if got.alternate != v.Alternate {
				t.Errorf("doubleMetaphone(%q).alternate = %q, want %q",
					v.Term, got.alternate, v.Alternate)
			}
		})
	}
}

// TestDoubleMetaphone_LetterFreeInput_YieldsEmptyCodes covers the
// degenerate input the matcher's phonetic tier guards on.
func TestDoubleMetaphone_LetterFreeInput_YieldsEmptyCodes(t *testing.T) {
	t.Parallel()
	got := doubleMetaphone("123 !!!")
	if got.primary != "" || got.alternate != "" {
		t.Errorf("doubleMetaphone(non-letters) = {%q,%q}, want empty",
			got.primary, got.alternate)
	}
}
