package zyins

import (
	"fmt"
	"testing"
)

func TestNewProductSelection_RejectsEmpty(t *testing.T) {
	if _, err := NewProductSelection(); err == nil {
		t.Errorf("expected error for empty selection")
	}
	if _, err := NewProductSelection(""); err == nil {
		t.Errorf("expected error for blank token")
	}
}

func TestNewProductSelection_RejectsSurroundingWhitespace(t *testing.T) {
	cases := []string{
		" colonial-penn.final-expense",
		"colonial-penn.final-expense ",
		"\tcolonial-penn.final-expense",
		"colonial-penn.final-expense\n",
		"  colonial-penn.final-expense  ",
	}
	for _, tok := range cases {
		t.Run(fmt.Sprintf("%q", tok), func(t *testing.T) {
			if _, err := NewProductSelection(tok); err == nil {
				t.Errorf("expected error for token with surrounding whitespace: %q", tok)
			}
		})
	}
}

func TestNewProductSelectionFromProducts_RejectsSurroundingWhitespace(t *testing.T) {
	p := Product{Brand: "x", Type: ProductFinalExpense, WireToken: " x.final-expense "}
	if _, err := NewProductSelectionFromProducts(p); err == nil {
		t.Errorf("expected error for product wire token with surrounding whitespace")
	}
}

func TestProductSelection_WireStringJoinsWithPipe(t *testing.T) {
	sel, err := NewProductSelection("a.b", "c.d", "e.f")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := sel.WireString(); got != "a.b|c.d|e.f" {
		t.Errorf("WireString = %q, want a.b|c.d|e.f", got)
	}
	if sel.Len() != 3 {
		t.Errorf("Len = %d, want 3", sel.Len())
	}
}

func TestProductSelection_FromProducts(t *testing.T) {
	p := Product{Brand: "x", Type: ProductFinalExpense, WireToken: "x.final-expense"}
	sel, err := NewProductSelectionFromProducts(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.WireString() != "x.final-expense" {
		t.Errorf("WireString = %q", sel.WireString())
	}
	out := sel.Products()
	if len(out) != 1 || out[0].Brand != "x" {
		t.Errorf("Products() = %+v", out)
	}
	// Mutating the returned slice must not affect the selection.
	out[0].Brand = "mutated"
	if sel.Products()[0].Brand != "x" {
		t.Errorf("ProductSelection leaked internal slice; brand mutated")
	}
}

func TestProductCatalog_TryFindReturnsCopy(t *testing.T) {
	catalog := DefaultProductCatalog()
	p := catalog.TryFind("colonial-penn", ProductFinalExpense)
	if p == nil {
		t.Fatalf("expected product")
	}

	p.Brand = "mutated"

	got, err := catalog.Find("colonial-penn", ProductFinalExpense)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Brand != "colonial-penn" {
		t.Errorf("TryFind leaked catalog storage; brand = %q", got.Brand)
	}
}

func TestProductCatalogFromDatasets_SkipsUnknownProductClass(t *testing.T) {
	catalog := ProductCatalogFromDatasets(map[string]any{
		"products": map[string]any{
			"unknown": []any{
				map[string]any{
					"identifier": "carrier.unknown",
					"carrier":    "carrier",
					"name":       "Unknown Product",
					"product":    "unexpected",
				},
			},
		},
	})

	if got := catalog.List(); len(got) != 0 {
		t.Fatalf("unknown product class was included: %+v", got)
	}
}
