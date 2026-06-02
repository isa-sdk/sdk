package zyins

// ProductType is the coarse product category. Wire format uses
// underscore-separated lowercase codes; values here map 1:1.
type ProductType string

const (
	ProductFinalExpense       ProductType = "final_expense"
	ProductTerm               ProductType = "term"
	ProductWholeLife          ProductType = "whole_life"
	ProductMedicareSupplement ProductType = "medicare_supplement"
	ProductUniversal          ProductType = "universal"
	ProductIndexed            ProductType = "indexed"
)
