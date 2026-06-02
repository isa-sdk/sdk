package catalog

import "testing"

func TestStates_ByAbbreviation(t *testing.T) {
	if s, ok := States.ByAbbreviation("nc"); !ok || s != StateNorthCarolina {
		t.Errorf("ByAbbreviation(nc)=%v ok=%v want %v", s, ok, StateNorthCarolina)
	}
	if s, ok := States.ByAbbreviation("North Carolina"); !ok || s != StateNorthCarolina {
		t.Errorf("ByAbbreviation(name)=%v ok=%v", s, ok)
	}
	if _, ok := States.ByAbbreviation("ZZ"); ok {
		t.Error("expected miss for ZZ")
	}
	if len(States.Values()) < 50 {
		t.Errorf("Values count=%d", len(States.Values()))
	}
}

func TestProducts_NonEmpty(t *testing.T) {
	all := ProductsAll()
	if len(all) == 0 {
		t.Fatal("ProductsAll returned empty — catalog may not have been generated")
	}
	for _, p := range all {
		if len(p.Id) < 10 || p.Id[:5] != "prod_" {
			t.Errorf("product %q has malformed Id %q (want prod_<uuid>)", p.Name, p.Id)
		}
		if p.Name == "" {
			t.Errorf("product %q has empty Name", p.Id)
		}
		if p.Class == "" {
			t.Errorf("product %q has empty Class", p.Id)
		}
	}
}

// TestProducts_ByID_RoundTrip is the required conformance test:
// ByID(Products.Fex.AetnaAccendo().Id) must return Products.Fex.AetnaAccendo(),
// and stale slugs / display names must NOT resolve (only prod_<uuid> ids do).
func TestProducts_ByID_RoundTrip(t *testing.T) {
	t.Parallel()
	ref := Products.Fex.AetnaAccendo()
	got, ok := ByID(ref.Id)
	if !ok {
		t.Fatalf("ByID(%q): product not found", ref.Id)
	}
	if got != ref {
		t.Errorf("ByID(%q) = %+v, want %+v", ref.Id, got, ref)
	}
	// Display name must not resolve — only prod_<uuid> ids are indexed.
	if _, ok := ByID("Aetna Accendo"); ok {
		t.Error("ByID(display name) returned a result — only prod_<uuid> ids are valid lookup keys")
	}
	// Slug must not resolve.
	if _, ok := ByID("fex-aetna-accendo"); ok {
		t.Error("ByID(slug) returned a result — slugs are not valid lookup keys")
	}
}

func TestStateMetadata_Lookup(t *testing.T) {
	m, ok := States.Metadata(StateCalifornia)
	if !ok {
		t.Fatal("expected California metadata")
	}
	if m.Name != "California" {
		t.Errorf("Metadata.Name=%q", m.Name)
	}
	if m.IsTerritory {
		t.Error("California should not be a territory")
	}
}

func TestMetadata_UnknownValuesReturnMiss(t *testing.T) {
	if _, ok := States.Metadata(State("ZZ")); ok {
		t.Error("expected unknown state miss")
	}
	// ByID with a well-formed but unknown id must miss.
	if _, ok := ByID("prod_00000000-0000-0000-0000-000000000000"); ok {
		t.Error("expected unknown product id miss")
	}
	if _, ok := Carriers.Metadata("unknown-carrier"); ok {
		t.Error("expected unknown carrier miss")
	}
}
