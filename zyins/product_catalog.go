package zyins

import (
	"fmt"
	"strings"

	"github.com/isa-sdk/sdk/catalog"
)

// ProductCatalog is an in-memory catalog of products fetched from the server.
// Construct via ProductCatalogFromDatasets (from a /v3/datasets response) or
// let ProductsService.Catalog do it automatically.
//
// For static, compile-time product access use catalog.Products directly;
// ProductCatalog is for callers who need to query the live server catalog.
type ProductCatalog struct {
	products []catalog.Product
}

// ProductCatalogFromDatasets builds a catalog from a datasets bundle returned by
//
//	client.DatasetsV3.Get(ctx, zyins.DatasetsV3Options{Include: []zyins.DatasetCategory{"products"}})
//
// Each entry is normalized to the prod_<uuid> form. Entries missing a non-empty
// id or name are silently skipped.
func ProductCatalogFromDatasets(bundle map[string]any) *ProductCatalog {
	raw, _ := bundle["products"].(map[string]any)
	if raw == nil {
		return &ProductCatalog{}
	}
	var products []catalog.Product
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

// Find returns the product matching the given display name (case-insensitive),
// or an error when no match exists.
func (c *ProductCatalog) Find(name string) (catalog.Product, error) {
	p := c.TryFind(name)
	if p == nil {
		return catalog.Product{}, fmt.Errorf("zyins: ProductCatalog.Find: no product matches name=%q", name)
	}
	return *p, nil
}

// TryFind returns the product matching the given display name
// (case-insensitive), or nil when no match exists.
func (c *ProductCatalog) TryFind(name string) *catalog.Product {
	needle := strings.ToLower(name)
	for i := range c.products {
		if strings.ToLower(c.products[i].Name) == needle {
			p := c.products[i]
			return &p
		}
	}
	return nil
}

// List returns all products in the catalog.
func (c *ProductCatalog) List() []catalog.Product {
	out := make([]catalog.Product, len(c.products))
	copy(out, c.products)
	return out
}

// rawEntryToProduct converts one raw dataset entry to a catalog.Product.
// Returns nil for entries lacking a non-empty id or name.
func rawEntryToProduct(entry map[string]any) *catalog.Product {
	id, _ := entry["id"].(string)
	name, _ := entry["name"].(string)
	carrier, _ := entry["carrier"].(string)
	cls, _ := entry["product"].(string)
	if cls == "" {
		cls, _ = entry["class"].(string)
	}
	if id == "" || name == "" {
		return nil
	}
	// Normalize id to the prod_<uuid> form the server emits on v3.
	if !strings.HasPrefix(id, "prod_") {
		id = "prod_" + id
	}
	return &catalog.Product{
		Id:      id,
		Name:    name,
		Class:   cls,
		Carrier: carrier,
	}
}
