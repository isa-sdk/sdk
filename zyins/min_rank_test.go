package zyins

import "testing"

func TestMinRank_CanonicalValuesMapToLowercaseWireTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  MinRank
		want string
	}{
		{"immediate", MinRankImmediate, "immediate"},
		{"graded", MinRankGraded, "graded"},
		{"rop", MinRankRop, "rop"},
		{"guaranteed", MinRankGuaranteed, "guaranteed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.got) != tt.want {
				t.Errorf("got %q, want %q", string(tt.got), tt.want)
			}
		})
	}
}

func TestMinRank_SynonymsCollapseOntoCanonicalToken(t *testing.T) {
	t.Parallel()
	if MinRankReturnOfPremium != MinRankRop {
		t.Errorf("MinRankReturnOfPremium = %q, want %q", MinRankReturnOfPremium, MinRankRop)
	}
	if MinRankGuaranteedIssue != MinRankGuaranteed {
		t.Errorf("MinRankGuaranteedIssue = %q, want %q", MinRankGuaranteedIssue, MinRankGuaranteed)
	}
	if MinRankGi != MinRankGuaranteed {
		t.Errorf("MinRankGi = %q, want %q", MinRankGi, MinRankGuaranteed)
	}
}

func TestMinRank_AssignsToStringOptionField(t *testing.T) {
	t.Parallel()
	// MinRank is a string type, so a typed constant and a plain string both
	// satisfy the option field — the non-breaking escape hatch.
	opts := PrequalifyV3Options{MinRank: string(MinRankGi)}
	if opts.MinRank != "guaranteed" {
		t.Errorf("MinRank = %q, want %q", opts.MinRank, "guaranteed")
	}
}

func TestAllMinRankValues_ReturnsFourCanonicalTokens(t *testing.T) {
	t.Parallel()
	got := AllMinRankValues()
	want := map[string]bool{"immediate": true, "graded": true, "rop": true, "guaranteed": true}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d: %v", len(got), len(want), got)
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("unexpected value %q", v)
		}
	}
}
