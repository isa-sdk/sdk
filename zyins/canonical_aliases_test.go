package zyins

import "testing"

// TestCanonicalSurface asserts the singular canonical fields exist on Client.
//
// Per the locked SDK syntax (TS canon):
//   - isa.zyins.license is canonical (singular only; no plural alias).
//   - isa.zyins.cases.share is canonical; isa.zyins.cases.create is a
//     deprecated alias for callers who had not yet migrated.
func TestCanonicalSurface(t *testing.T) {
	c, err := NewClient(WithToken("isa_test_4fjK2nQ7mX1aB8sR9pZ3"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if c.License == nil {
		t.Fatal("zyins.Client.License is nil")
	}

	if c.Cases == nil {
		t.Fatal("zyins.Client.Cases is nil")
	}
	// Share method must be callable — compile-time check via an indirect call.
	// We use a local variable so the compiler cannot fold it away, and we
	// never actually call it (no live server in unit tests).
	var _ = c.Cases.Share
	var _ = c.Cases.Create
}
