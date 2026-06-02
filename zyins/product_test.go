package zyins

import (
	"testing"

	"github.com/isa-sdk/sdk/catalog"
)

func TestNewProductSelectionOf_RejectsEmpty(t *testing.T) {
	if _, err := NewProductSelectionOf(); err == nil {
		t.Errorf("expected error for empty selection")
	}
}

func TestNewProductSelectionOf_RejectsIdlessProduct(t *testing.T) {
	p := catalog.Product{Name: "No ID Product", Class: "fex"}
	if _, err := NewProductSelectionOf(p); err == nil {
		t.Errorf("expected error for product with empty Id")
	}
}

func TestNewProductSelectionOf_RejectsLegacySlugId(t *testing.T) {
	// A legacy slug like "fidelity-life-instabrain-pure-term" is not a prod_<uuid>;
	// the v3 wire contract only accepts prod_<uuid> identifiers.
	p := catalog.Product{Id: "fidelity-life-instabrain-pure-term", Name: "Fidelity Life", Class: "term"}
	if _, err := NewProductSelectionOf(p); err == nil {
		t.Errorf("expected error for product with non-prod_ Id")
	}
}

func TestNewProductSelectionOf_AcceptsCatalogProduct(t *testing.T) {
	sel, err := NewProductSelectionOf(catalog.Products.Fex.AetnaAccendo())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Len() != 1 {
		t.Errorf("Len = %d, want 1", sel.Len())
	}
	ids := sel.wireIDs()
	if len(ids) != 1 || ids[0] != catalog.Products.Fex.AetnaAccendo().Id {
		t.Errorf("wireIDs = %v, want [%q]", ids, catalog.Products.Fex.AetnaAccendo().Id)
	}
}

func TestNewProductSelectionOf_WireIDsAreProdUUIDs(t *testing.T) {
	sel, err := NewProductSelectionOf(
		catalog.Products.Fex.AetnaAccendo(),
		catalog.Products.Term.BannerOpterm(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := sel.wireIDs()
	if len(ids) != 2 {
		t.Fatalf("wireIDs length = %d, want 2", len(ids))
	}
	for _, id := range ids {
		if len(id) < 10 || id[:5] != "prod_" {
			t.Errorf("wireID %q is not a prod_<uuid>", id)
		}
	}
}

func TestProductSelection_Items_ReturnsCopy(t *testing.T) {
	p := catalog.Products.Fex.AetnaAccendo()
	sel, err := NewProductSelectionOf(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := sel.Items()
	if len(items) != 1 || items[0] != p {
		t.Errorf("Items() = %+v", items)
	}
	// Mutating the returned slice must not affect the selection.
	items[0] = catalog.Product{Id: "prod_mutated"}
	if sel.wireIDs()[0] != p.Id {
		t.Errorf("ProductSelection leaked internal slice")
	}
}

func TestProductCatalogFromDatasets_SkipsEntriesMissingID(t *testing.T) {
	c := ProductCatalogFromDatasets(map[string]any{
		"products": map[string]any{
			"fex": []any{
				map[string]any{
					// no "id" field — must be skipped
					"name":    "Some Product",
					"carrier": "Some Carrier",
					"class":   "fex",
				},
			},
		},
	})
	if got := c.List(); len(got) != 0 {
		t.Fatalf("expected empty catalog for entry with no id; got %+v", got)
	}
}

func TestProductCatalogFromDatasets_ParsesIDWithProdPrefix(t *testing.T) {
	c := ProductCatalogFromDatasets(map[string]any{
		"products": map[string]any{
			"fex": []any{
				map[string]any{
					"id":      "d7b57156-3e83-506b-8936-0692c1193dc7",
					"name":    "Aetna Accendo",
					"carrier": "Aetna",
					"class":   "fex",
				},
			},
		},
	})
	list := c.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 product; got %d", len(list))
	}
	if list[0].Id != "prod_d7b57156-3e83-506b-8936-0692c1193dc7" {
		t.Errorf("Id = %q, want prod_ prefix", list[0].Id)
	}
}
