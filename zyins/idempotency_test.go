package zyins

import "testing"

func TestGenerateIdempotencyKey_UniqueLengthHex(t *testing.T) {
	a, err := generateIdempotencyKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := generateIdempotencyKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a) != idempotencyKeyLen*2 {
		t.Errorf("len = %d, want %d", len(a), idempotencyKeyLen*2)
	}
	if a == b {
		t.Errorf("expected unique keys; both = %q", a)
	}
}

func TestDeriveIdempotencyKey_DeterministicAcrossCalls(t *testing.T) {
	a := deriveIdempotencyKey("isa_test_x", "prequalify", []byte(`{"a":1}`))
	b := deriveIdempotencyKey("isa_test_x", "prequalify", []byte(`{"a":1}`))
	if a != b {
		t.Errorf("expected stable key; got %q vs %q", a, b)
	}
}

func TestDeriveIdempotencyKey_DifferentInputsDiffer(t *testing.T) {
	a := deriveIdempotencyKey("scope1", "op", []byte("body"))
	b := deriveIdempotencyKey("scope2", "op", []byte("body"))
	if a == b {
		t.Errorf("expected different keys for different scopes")
	}
}
