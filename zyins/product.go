package zyins

import (
	"errors"
	"fmt"
	"strings"
)

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

// Product is a single carrier/type combination offered through the
// prequalify engine.
type Product struct {
	// Brand is the carrier identifier (e.g., "colonial-penn").
	Brand string `json:"brand"`
	// Type is the product category.
	Type ProductType `json:"type"`
	// WireToken is the engine's canonical brand-and-type string. Stable
	// within a wire major version.
	WireToken string `json:"wire_token"`
	// DisplayName is the human-readable label for UI surfaces.
	DisplayName string `json:"display_name,omitempty"`
}

// ProductSelection groups one or more products for a single prequalify
// call. WireString renders the `|`-joined token expression the engine
// accepts so call sites never assemble it manually.
type ProductSelection struct {
	products []Product
}

// NewProductSelection constructs a selection from one or more wire
// tokens. The tokens are not validated against the catalog; callers
// with a typed Product should use NewProductSelectionFromProducts.
//
// Tokens with surrounding whitespace are rejected (not trimmed): the
// wire contract treats the token as opaque, and silently trimming
// would mean the stored value diverges from what the caller passed
// — a hard-to-diagnose source of "why doesn't this match the catalog"
// reports.
func NewProductSelection(tokens ...string) (ProductSelection, error) {
	if len(tokens) == 0 {
		return ProductSelection{}, errors.New("zyins: NewProductSelection requires at least one token")
	}
	products := make([]Product, 0, len(tokens))
	for _, t := range tokens {
		trimmed := strings.TrimSpace(t)
		if len(trimmed) == 0 {
			return ProductSelection{}, errors.New("zyins: product token cannot be empty")
		}
		if len(trimmed) != len(t) {
			return ProductSelection{}, errors.New("zyins: product token has leading or trailing whitespace; trim before passing to the SDK")
		}
		products = append(products, Product{WireToken: t})
	}
	return ProductSelection{products: products}, nil
}

// NewProductSelectionFromProducts constructs a selection from a list of
// typed Product values.
func NewProductSelectionFromProducts(products ...Product) (ProductSelection, error) {
	if len(products) == 0 {
		return ProductSelection{}, errors.New("zyins: NewProductSelectionFromProducts requires at least one product")
	}
	for _, p := range products {
		trimmed := strings.TrimSpace(p.WireToken)
		if len(trimmed) == 0 {
			return ProductSelection{}, errors.New("zyins: product wire token cannot be empty")
		}
		if len(trimmed) != len(p.WireToken) {
			return ProductSelection{}, errors.New("zyins: product wire token has leading or trailing whitespace; trim before passing to the SDK")
		}
	}
	out := make([]Product, len(products))
	copy(out, products)
	return ProductSelection{products: out}, nil
}

// WireTokens returns the product wire tokens as a string slice for the
// 0.5.1 flat wire body's `products` field.
func (p ProductSelection) WireTokens() []string {
	tokens := make([]string, len(p.products))
	for i, prod := range p.products {
		tokens[i] = prod.WireToken
	}
	return tokens
}

// WireString renders the prequalify wire string: a `|`-joined list of
// product tokens.
//
// Deprecated: Use WireTokens() which returns []string matching the
// 0.5.1 wire body's products field. Will be removed in v0.7.0.
func (p ProductSelection) WireString() string {
	return strings.Join(p.WireTokens(), "|")
}

// Products returns a read-only view of the selection contents.
func (p ProductSelection) Products() []Product {
	out := make([]Product, len(p.products))
	copy(out, p.products)
	return out
}

// Len returns the number of products in the selection.
func (p ProductSelection) Len() int { return len(p.products) }

// ProductCatalog is an in-memory catalog of known products.
// Construct via DefaultProductCatalog or ProductCatalogFromDatasets.
type ProductCatalog struct {
	products []Product
}

// DefaultProductCatalog returns the static built-in catalog shipped
// with the SDK.
func DefaultProductCatalog() *ProductCatalog {
	return &ProductCatalog{products: defaultProducts()}
}

// ProductCatalogFromDatasets builds a catalog from a datasets bundle
// returned by `client.Datasets.Get(ctx, include: []string{"products"})`.
// The products field is a map of product-class keys to arrays of raw
// product entry objects. Entries missing required fields are skipped.
func ProductCatalogFromDatasets(bundle map[string]any) *ProductCatalog {
	raw, _ := bundle["products"].(map[string]any)
	if raw == nil {
		return &ProductCatalog{}
	}
	var products []Product
	for _, v := range raw {
		entries, ok := v.([]any)
		if !ok {
			continue
		}
		for _, e := range entries {
			entry, ok := e.(map[string]any)
			if !ok {
				continue
			}
			p := rawEntryToProduct(entry)
			if p != nil {
				products = append(products, *p)
			}
		}
	}
	return &ProductCatalog{products: products}
}

// Find returns the product matching brand and type, or an error.
func (c *ProductCatalog) Find(brand string, t ProductType) (Product, error) {
	p := c.TryFind(brand, t)
	if p == nil {
		return Product{}, fmt.Errorf("zyins: ProductCatalog.Find: no product matches brand=%q type=%q", brand, string(t))
	}
	return *p, nil
}

// TryFind returns the product matching brand and type, or nil.
func (c *ProductCatalog) TryFind(brand string, t ProductType) *Product {
	for i := range c.products {
		if c.products[i].Brand == brand && c.products[i].Type == t {
			p := c.products[i]
			return &p
		}
	}
	return nil
}

// FindBySlug returns the product matching the wire token slug, or an error.
func (c *ProductCatalog) FindBySlug(slug string) (Product, error) {
	p := c.TryFindBySlug(slug)
	if p == nil {
		return Product{}, fmt.Errorf("zyins: ProductCatalog.FindBySlug: no product matches slug=%q", slug)
	}
	return *p, nil
}

// TryFindBySlug returns the product matching the wire token slug, or nil.
func (c *ProductCatalog) TryFindBySlug(slug string) *Product {
	for i := range c.products {
		if c.products[i].WireToken == slug {
			p := c.products[i]
			return &p
		}
	}
	return nil
}

// List returns all products in the catalog.
func (c *ProductCatalog) List() []Product {
	out := make([]Product, len(c.products))
	copy(out, c.products)
	return out
}

func rawEntryToProduct(entry map[string]any) *Product {
	identifier, _ := entry["identifier"].(string)
	carrier, _ := entry["carrier"].(string)
	name, _ := entry["name"].(string)
	if identifier == "" || carrier == "" || name == "" {
		return nil
	}
	productClass, _ := entry["product"].(string)
	productType, ok := mapProductClass(productClass)
	if !ok {
		return nil
	}
	return &Product{
		Brand:       carrier,
		Type:        productType,
		WireToken:   identifier,
		DisplayName: name,
	}
}

func mapProductClass(cls string) (ProductType, bool) {
	switch strings.ToLower(cls) {
	case "fex":
		return ProductFinalExpense, true
	case "term":
		return ProductTerm, true
	case "wl", "whole_life", "wholelife":
		return ProductWholeLife, true
	case "medsup", "medicare_supplement":
		return ProductMedicareSupplement, true
	case "ul", "universal":
		return ProductUniversal, true
	case "indexed":
		return ProductIndexed, true
	default:
		return "", false
	}
}

func defaultProducts() []Product {
	return []Product{
		{Brand: "colonial-penn", Type: ProductFinalExpense, WireToken: "colonial-penn.final-expense", DisplayName: "Colonial Penn Final Expense"},
		{Brand: "mutual-of-omaha", Type: ProductFinalExpense, WireToken: "mutual-of-omaha.final-expense", DisplayName: "Mutual of Omaha Final Expense"},
		{Brand: "aetna", Type: ProductMedicareSupplement, WireToken: "aetna.medicare-supplement", DisplayName: "Aetna Medicare Supplement"},
	}
}
