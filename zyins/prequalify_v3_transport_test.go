package zyins

import "testing"

func TestDecodePrequalifyV3Envelope_MissingLivemodeDefaultsFalse(t *testing.T) {
	const responseBody = `{"data":{"plans":[]},"request_id":"req_123"}`

	got, err := decodePrequalifyV3Envelope([]byte(responseBody), "idem_123")
	if err != nil {
		t.Fatalf("decodePrequalifyV3Envelope: %v", err)
	}
	if got.Livemode {
		t.Fatal("expected missing livemode to default false")
	}
}

func TestDecodeQuoteV3Envelope_MissingLivemodeDefaultsFalse(t *testing.T) {
	const responseBody = `{"data":{"plans":[]},"request_id":"req_123"}`

	got, err := decodeQuoteV3Envelope([]byte(responseBody), "idem_123")
	if err != nil {
		t.Fatalf("decodeQuoteV3Envelope: %v", err)
	}
	if got.Livemode {
		t.Fatal("expected missing livemode to default false")
	}
}

func TestDecodeV3Plans_AbsentPlansField_ReturnsError(t *testing.T) {
	// Missing plans key (wire-shape drift) should fail, not silently return empty.
	const dataWithoutPlans = `{"other_field": "value"}`

	_, err := decodeV3Plans([]byte(dataWithoutPlans))
	if err == nil {
		t.Fatal("expected error when plans field is absent, got nil")
	}
	const wantErr = "missing plans field"
	if gotErr := err.Error(); !containsSubstr(gotErr, wantErr) {
		t.Errorf("expected error containing %q, got: %s", wantErr, gotErr)
	}
}

func TestDecodeV3Plans_EmptyPlansArray_ReturnsEmptySlice(t *testing.T) {
	// Present-but-empty plans is valid (no offers).
	const dataWithEmptyPlans = `{"plans":[]}`

	got, err := decodeV3Plans([]byte(dataWithEmptyPlans))
	if err != nil {
		t.Fatalf("decodeV3Plans with empty array: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d offers", len(got))
	}
}

func containsSubstr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstr(s, substr)))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
