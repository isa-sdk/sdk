package zyins

import "testing"

func TestBundledAPIVersions_DeclaresAllExpectedSurfaces(t *testing.T) {
	expected := map[string]string{
		"prequalify": "v2",
		"quote":      "v2",
		"datasets":   "v2",
		"reference":  "v2",
		"sessions":   "v1",
		"branding":   "v1",
		"cases":      "v1",
	}
	for surface, want := range expected {
		got, ok := BundledAPIVersions[surface]
		if !ok {
			t.Errorf("BundledAPIVersions missing surface %q", surface)
			continue
		}
		if got != want {
			t.Errorf("BundledAPIVersions[%q] = %q, want %q", surface, got, want)
		}
	}
}

func TestResolveAPIVersion(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		surface   string
		want      string
	}{
		{
			name:    "bundled fallback for prequalify",
			surface: "prequalify",
			want:    "v2",
		},
		{
			name:      "override beats bundled",
			overrides: map[string]string{"quote": "v3"},
			surface:   "quote",
			want:      "v3",
		},
		{
			name:      "override unrelated surface falls back to bundled",
			overrides: map[string]string{"prequalify": "v3"},
			surface:   "quote",
			want:      "v2",
		},
		{
			name:    "unknown surface returns empty string",
			surface: "totally_made_up",
			want:    "",
		},
		{
			name:      "empty-string override falls through to bundled",
			overrides: map[string]string{"quote": ""},
			surface:   "quote",
			want:      "v2",
		},
		{
			name:    "nil overrides safe",
			surface: "branding",
			want:    "v1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAPIVersion(tt.overrides, tt.surface)
			if got != tt.want {
				t.Errorf("ResolveAPIVersion(%v, %q) = %q, want %q", tt.overrides, tt.surface, got, tt.want)
			}
		})
	}
}
