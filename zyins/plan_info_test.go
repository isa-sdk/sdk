package zyins

import (
	"reflect"
	"testing"
)

// Tests for the typed plan-info surface + Title Case label derivation.
// Mirrors packages/python/tests/zyins/test_plan_info_label.py and the
// TS planInfoLabel.test.ts coverage.

func TestTitleCaseLabel_SpecialAcronyms(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"eapp", "eApp"},
		{"EApp", "eApp"},
		{"EAPP", "eApp"},
		{"url", "URL"},
		{"pdf", "PDF"},
		{"api", "API"},
		{"ssn", "SSN"},
		{"ach", "ACH"},
		{"eft", "EFT"},
		{"id", "ID"},
		{"faq", "FAQ"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			if got := TitleCaseLabel(c.key); got != c.want {
				t.Errorf("TitleCaseLabel(%q) = %q, want %q", c.key, got, c.want)
			}
		})
	}
}

func TestTitleCaseLabel_GenericSnakeKebabCase(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"rate_class", "Rate Class"},
		{"rate_class_notes", "Rate Class Notes"},
		{"telesales", "Telesales"},
		{"max-issue-age", "Max Issue Age"},
		{"face_amount_max", "Face Amount Max"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			if got := TitleCaseLabel(c.key); got != c.want {
				t.Errorf("TitleCaseLabel(%q) = %q, want %q", c.key, got, c.want)
			}
		})
	}
}

func TestTitleCaseLabel_SpecialTokenInsideCompoundKey(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"api_url", "API URL"},
		{"eapp_telesales", "eApp Telesales"},
		{"submit_pdf", "Submit PDF"},
	}
	for _, c := range cases {
		if got := TitleCaseLabel(c.key); got != c.want {
			t.Errorf("TitleCaseLabel(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestTitleCaseLabel_EmptyString(t *testing.T) {
	if got := TitleCaseLabel(""); got != "" {
		t.Errorf("TitleCaseLabel(\"\") = %q, want \"\"", got)
	}
}

func TestTitleCaseLabel_ConsecutiveSeparators(t *testing.T) {
	// foo__bar and foo--bar produce the same label as foo_bar;
	// consecutive separators collapse into one split.
	cases := []struct{ key, want string }{
		{"foo__bar", "Foo Bar"},
		{"foo--bar", "Foo Bar"},
		{"foo_-bar", "Foo Bar"},
	}
	for _, c := range cases {
		if got := TitleCaseLabel(c.key); got != c.want {
			t.Errorf("TitleCaseLabel(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestCoercePlanInfo_TypedArrayUsedVerbatim(t *testing.T) {
	wire := []any{
		map[string]any{
			"key":    "eapp",
			"label":  "eApp",
			"values": []any{"yes"},
		},
		map[string]any{
			"key":    "telesales",
			"label":  "Telesales",
			"values": []any{"no"},
		},
	}
	got := CoercePlanInfo(wire)
	want := []PlanInfoItem{
		{Key: "eapp", Label: "eApp", Values: []string{"yes"}},
		{Key: "telesales", Label: "Telesales", Values: []string{"no"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CoercePlanInfo:\n got %+v\nwant %+v", got, want)
	}
}

func TestCoercePlanInfo_TypedArraySynthesizesLabel(t *testing.T) {
	wire := []any{
		map[string]any{"key": "rate_class_notes", "values": []any{"A"}},
	}
	got := CoercePlanInfo(wire)
	if len(got) != 1 || got[0].Label != "Rate Class Notes" {
		t.Errorf("synthesized label: got %+v", got)
	}
}

func TestCoercePlanInfo_TypedMapArrayUsedVerbatim(t *testing.T) {
	wire := []map[string]any{
		{"key": "eapp", "label": "eApp", "values": []string{"yes"}},
		{"key": "telesales", "label": "Telesales", "values": []string{"no"}},
	}
	got := CoercePlanInfo(wire)
	want := []PlanInfoItem{
		{Key: "eapp", Label: "eApp", Values: []string{"yes"}},
		{Key: "telesales", Label: "Telesales", Values: []string{"no"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CoercePlanInfo:\n got %+v\nwant %+v", got, want)
	}
}

func TestCoercePlanInfo_TypedArraySkipsEntriesWithoutKey(t *testing.T) {
	wire := []any{
		map[string]any{"label": "Orphan", "values": []any{}},
		map[string]any{"key": "eapp", "values": []any{}},
	}
	got := CoercePlanInfo(wire)
	if len(got) != 1 || got[0].Key != "eapp" {
		t.Errorf("expected one entry with key=eapp, got %+v", got)
	}
}

func TestCoercePlanInfo_LegacyMapUpconverts(t *testing.T) {
	wire := map[string]any{
		"rate_class": []any{"preferred"},
		"eapp":       []string{"yes"},
	}
	got := CoercePlanInfo(wire)
	want := []PlanInfoItem{
		{Key: "eapp", Label: "eApp", Values: []string{"yes"}},
		{Key: "rate_class", Label: "Rate Class", Values: []string{"preferred"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CoercePlanInfo:\n got %+v\nwant %+v", got, want)
	}
}

func TestCoercePlanInfo_UnknownShapeReturnsNil(t *testing.T) {
	cases := []any{nil, "string", 42, 3.14}
	for _, c := range cases {
		if got := CoercePlanInfo(c); got != nil {
			t.Errorf("CoercePlanInfo(%v) = %+v, want nil", c, got)
		}
	}
}

func TestCoercePlanInfo_NonStringValuesDropped(t *testing.T) {
	wire := []any{
		map[string]any{"key": "eapp", "values": []any{"yes", 42, nil, "no"}},
	}
	got := CoercePlanInfo(wire)
	want := []string{"yes", "no"}
	if !reflect.DeepEqual(got[0].Values, want) {
		t.Errorf("values: got %+v, want %+v", got[0].Values, want)
	}
}

func TestCoercePlanInfo_WireOrderPreserved(t *testing.T) {
	wire := []any{
		map[string]any{"key": "z", "values": []any{}},
		map[string]any{"key": "a", "values": []any{}},
		map[string]any{"key": "m", "values": []any{}},
	}
	got := CoercePlanInfo(wire)
	keys := []string{got[0].Key, got[1].Key, got[2].Key}
	want := []string{"z", "a", "m"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("wire order: got %+v, want %+v", keys, want)
	}
}
