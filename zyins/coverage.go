package zyins

import (
	"errors"
	"fmt"
)

// CoverageType is the discriminator for the Coverage union.
type CoverageType string

const (
	// CoverageFaceValue requests coverage by death benefit (USD).
	CoverageFaceValue CoverageType = "face_value"
	// CoverageMonthlyBudget requests coverage by monthly premium (USD).
	CoverageMonthlyBudget CoverageType = "monthly_budget"
)

// Coverage represents the desired coverage in either face-value or
// monthly-budget form, for either a single amount or several amounts
// probed in one call. Construct via NewFaceValueCoverage /
// NewMonthlyBudgetCoverage (single) or NewFaceValuesCoverage /
// NewMonthlyBudgetsCoverage (multi) so the type and amount(s) are paired
// correctly.
type Coverage struct {
	// Type is the discriminator. Set by the constructors.
	Type CoverageType `json:"type"`
	// Amount is the whole-dollar amount for a single-amount coverage.
	// Face value (death benefit) for type=face_value; monthly premium
	// ceiling for type=monthly_budget. Zero for a multi-amount coverage.
	Amount int `json:"amount"`
	// Amounts holds the whole-dollar amounts for a multi-amount coverage
	// (a single /v3/prequalify call probing several coverage levels).
	// Empty for a single-amount coverage; the multi constructors set it.
	Amounts []int `json:"amounts,omitempty"`
}

// NewFaceValueCoverage requests coverage by the dollar amount of death
// benefit. The amount must be a positive integer.
func NewFaceValueCoverage(amount int) (Coverage, error) {
	if amount <= 0 {
		return Coverage{}, errors.New("zyins: NewFaceValueCoverage requires a positive amount")
	}
	return Coverage{Type: CoverageFaceValue, Amount: amount}, nil
}

// NewMonthlyBudgetCoverage requests coverage by the monthly premium the
// applicant can afford. The amount must be a positive integer.
func NewMonthlyBudgetCoverage(amount int) (Coverage, error) {
	if amount <= 0 {
		return Coverage{}, errors.New("zyins: NewMonthlyBudgetCoverage requires a positive amount")
	}
	return Coverage{Type: CoverageMonthlyBudget, Amount: amount}, nil
}

// NewFaceValuesCoverage probes several face-value (death-benefit) amounts
// in one call. Each amount must be a positive integer; at least one is
// required.
func NewFaceValuesCoverage(amounts []int) (Coverage, error) {
	return newMultiCoverage(CoverageFaceValue, "NewFaceValuesCoverage", amounts)
}

// NewMonthlyBudgetsCoverage probes several monthly-premium ceilings in one
// call. Each amount must be a positive integer; at least one is required.
func NewMonthlyBudgetsCoverage(amounts []int) (Coverage, error) {
	return newMultiCoverage(CoverageMonthlyBudget, "NewMonthlyBudgetsCoverage", amounts)
}

func newMultiCoverage(t CoverageType, ctor string, amounts []int) (Coverage, error) {
	if len(amounts) == 0 {
		return Coverage{}, fmt.Errorf("zyins: %s requires at least one amount", ctor)
	}
	out := make([]int, len(amounts))
	for i, a := range amounts {
		if a <= 0 {
			return Coverage{}, fmt.Errorf("zyins: %s requires positive amounts", ctor)
		}
		out[i] = a
	}
	return Coverage{Type: t, Amounts: out}, nil
}

// IsMulti reports whether the coverage probes several amounts in one call.
func (c Coverage) IsMulti() bool { return len(c.Amounts) > 0 }

// IsFaceValue reports whether the coverage requests a death-benefit
// amount.
func (c Coverage) IsFaceValue() bool { return c.Type == CoverageFaceValue }

// IsMonthlyBudget reports whether the coverage requests a premium
// ceiling.
func (c Coverage) IsMonthlyBudget() bool { return c.Type == CoverageMonthlyBudget }

// validate returns nil when the coverage is well-formed.
func (c Coverage) validate() error {
	switch c.Type {
	case CoverageFaceValue, CoverageMonthlyBudget:
	default:
		return errors.New("zyins: coverage type must be face_value or monthly_budget")
	}
	if c.IsMulti() {
		for _, a := range c.Amounts {
			if a <= 0 {
				return errors.New("zyins: coverage amounts must be positive")
			}
		}
		return nil
	}
	if c.Amount <= 0 {
		return errors.New("zyins: coverage amount must be positive")
	}
	return nil
}
