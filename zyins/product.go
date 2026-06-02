package zyins

import (
	"errors"
	"fmt"
	"strings"

	"github.com/isa-sdk/sdk/catalog"
)

// ProductSelection groups one or more products for a single prequalify or quote
// call. Construct via NewProductSelectionOf; the selection serializes each
// product's Id (prod_<uuid>) onto the wire — never a slug.
//
// An idless product is a hard error at construction time, not a silent
// zero-plan surprise.
type ProductSelection struct {
	products []catalog.Product
}

// NewProductSelectionOf constructs a ProductSelection from one or more typed
// catalog.Product values. Each product must carry a non-empty Id (prod_<uuid>);
// the constructor returns an error otherwise so callers discover the problem
// immediately rather than receiving zero plans from the server.
//
// The canonical form is:
//
//	sel, err := zyins.NewProductSelectionOf(catalog.Products.Fex.AetnaAccendo())
func NewProductSelectionOf(products ...catalog.Product) (ProductSelection, error) {
	if len(products) == 0 {
		return ProductSelection{}, errors.New("zyins: NewProductSelectionOf requires at least one product")
	}
	for _, p := range products {
		if p.Id == "" {
			return ProductSelection{}, fmt.Errorf(
				"zyins: product %q has no Id — use catalog constants from catalog.Products; do not construct Product values manually",
				p.Name,
			)
		}
		if !strings.HasPrefix(p.Id, "prod_") {
			return ProductSelection{}, fmt.Errorf(
				"zyins: product %q has a non-canonical Id %q — use catalog constants from catalog.Products; slugs and legacy tokens are not accepted on the v3 wire",
				p.Name, p.Id,
			)
		}
	}
	out := make([]catalog.Product, len(products))
	copy(out, products)
	return ProductSelection{products: out}, nil
}

// Len returns the number of products in the selection.
func (p ProductSelection) Len() int { return len(p.products) }

// Items returns a read-only copy of the selected catalog.Product values.
func (p ProductSelection) Items() []catalog.Product {
	out := make([]catalog.Product, len(p.products))
	copy(out, p.products)
	return out
}

// wireIDs returns the prod_<uuid> id string for each selected product.
// This is the only wire value the v3 prequalify and quote endpoints accept.
// Unexported: call sites inside this package use it directly.
func (p ProductSelection) wireIDs() []string {
	ids := make([]string, len(p.products))
	for i, prod := range p.products {
		ids[i] = prod.Id
	}
	return ids
}
