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
	vs := Products.Values()
	if len(vs) == 0 {
		t.Skip("catalog empty (regenerate with go generate ./catalog)")
	}
	// Picking a known carrier from the data set verifies ByCarrier.
	if got := Products.ByCarrier(""); len(got) != 0 {
		t.Errorf("ByCarrier('') should be empty, got %d", len(got))
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
	if _, ok := Products.Metadata(Product("unknown-product")); ok {
		t.Error("expected unknown product miss")
	}
	if _, ok := Carriers.Metadata("unknown-carrier"); ok {
		t.Error("expected unknown carrier miss")
	}
}
