package zyins

import "testing"

func TestNewFaceValueCoverage(t *testing.T) {
	c, err := NewFaceValueCoverage(100_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.IsFaceValue() || c.Amount != 100_000 {
		t.Errorf("unexpected coverage: %+v", c)
	}
}

func TestNewFaceValueCoverage_PositiveOnly(t *testing.T) {
	if _, err := NewFaceValueCoverage(0); err == nil {
		t.Errorf("expected error for zero amount")
	}
	if _, err := NewFaceValueCoverage(-1); err == nil {
		t.Errorf("expected error for negative amount")
	}
}

func TestNewMonthlyBudgetCoverage(t *testing.T) {
	c, err := NewMonthlyBudgetCoverage(50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.IsMonthlyBudget() || c.Amount != 50 {
		t.Errorf("unexpected coverage: %+v", c)
	}
}

func TestCoverage_Validate(t *testing.T) {
	if err := (Coverage{Type: CoverageFaceValue, Amount: 1}).validate(); err != nil {
		t.Errorf("valid face value rejected: %v", err)
	}
	if err := (Coverage{Type: "garbage", Amount: 1}).validate(); err == nil {
		t.Errorf("unknown type should fail")
	}
	if err := (Coverage{Type: CoverageFaceValue, Amount: 0}).validate(); err == nil {
		t.Errorf("zero amount should fail")
	}
}
