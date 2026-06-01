package zyins

// AllSexValues returns every canonical wire value for the Sex enum.
//
// Go's type system represents enums as typed string constants; there is no
// reflection path to enumerate them.  This helper is the single source of
// truth for the full Sex value set, consumed by the conformance enum-parity
// harness to verify cross-language consistency.
func AllSexValues() []string {
	return []string{
		string(SexFemale),
		string(SexMale),
	}
}

// AllNicotineUsageValues returns every canonical wire value for NicotineUsage.
func AllNicotineUsageValues() []string {
	return []string{
		string(NicotineCurrent),
		string(NicotineFormer),
		string(NicotineNone),
	}
}

// AllProductTypeValues returns every canonical wire value for ProductType.
func AllProductTypeValues() []string {
	return []string{
		string(ProductFinalExpense),
		string(ProductIndexed),
		string(ProductMedicareSupplement),
		string(ProductTerm),
		string(ProductUniversal),
		string(ProductWholeLife),
	}
}

// AllCoverageTypeValues returns every canonical wire value for CoverageType.
func AllCoverageTypeValues() []string {
	return []string{
		string(CoverageFaceValue),
		string(CoverageMonthlyBudget),
	}
}
