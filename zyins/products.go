package zyins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

const (
	productDatasetKey = "products"
	datasetDataKey    = "data"
)

// productsPath is the GET endpoint for the products dataset.
const productsPath = "/v2/datasets/products"

// ProductsService provides a memoized product catalog fetched once from
// the server and reused across calls. Access via Client.Products.
type ProductsService struct {
	client *Client

	mu       sync.Mutex
	catalog  *ProductCatalog
	inflight *productCatalogFetch
}

type productCatalogFetch struct {
	done    chan struct{}
	catalog *ProductCatalog
	err     error
}

// Catalog returns the cached product catalog, fetching it from the
// server on the first call. Subsequent calls return the cached value
// without a network round-trip until Refresh is called.
func (s *ProductsService) Catalog(ctx context.Context) (*ProductCatalog, error) {
	s.mu.Lock()
	if s.catalog != nil {
		c := s.catalog
		s.mu.Unlock()
		return c, nil
	}
	if s.inflight != nil {
		f := s.inflight
		s.mu.Unlock()
		return waitProductCatalog(ctx, f, "Products.Catalog")
	}
	f := &productCatalogFetch{done: make(chan struct{})}
	s.inflight = f
	s.mu.Unlock()

	catalog, err := s.fetch(ctx)
	s.completeFetch(f, catalog, err)
	return catalog, err
}

// Refresh discards the cached catalog and fetches a fresh copy from
// the server. Safe to call concurrently; only one fetch runs at a time.
func (s *ProductsService) Refresh(ctx context.Context) (*ProductCatalog, error) {
	s.mu.Lock()
	if s.inflight != nil {
		f := s.inflight
		s.mu.Unlock()
		return waitProductCatalog(ctx, f, "Products.Refresh")
	}
	s.catalog = nil
	f := &productCatalogFetch{done: make(chan struct{})}
	s.inflight = f
	s.mu.Unlock()
	catalog, err := s.fetch(ctx)
	s.completeFetch(f, catalog, err)
	return catalog, err
}

func waitProductCatalog(ctx context.Context, f *productCatalogFetch, op string) (*ProductCatalog, error) {
	select {
	case <-f.done:
		return f.catalog, f.err
	case <-ctx.Done():
		return nil, fmt.Errorf("zyins: %s: %w", op, ctx.Err())
	}
}

func (s *ProductsService) completeFetch(f *productCatalogFetch, catalog *ProductCatalog, err error) {
	s.mu.Lock()
	f.catalog = catalog
	f.err = err
	if err == nil {
		s.catalog = catalog
	}
	close(f.done)
	if s.inflight == f {
		s.inflight = nil
	}
	s.mu.Unlock()
}

// fetch performs the actual network request and caches the result.
func (s *ProductsService) fetch(ctx context.Context) (*ProductCatalog, error) {
	raw, err := s.client.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   productsPath,
		op:     "products_catalog",
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Products.Catalog: %w", err)
	}

	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("zyins: Products.Catalog: failed to decode envelope: %w", err)
	}
	target := raw
	if len(env.Data) > 0 && !isJSONNull(env.Data) {
		target = env.Data
	}

	var bundle map[string]any
	if err := json.Unmarshal(target, &bundle); err != nil {
		return nil, fmt.Errorf("zyins: Products.Catalog: failed to decode datasets bundle: %w", err)
	}

	if data, ok := bundle[datasetDataKey].(map[string]any); ok {
		bundle = data
	}
	if _, ok := bundle[productDatasetKey]; !ok {
		bundle = map[string]any{productDatasetKey: bundle}
	}
	catalog := ProductCatalogFromDatasets(bundle)
	return catalog, nil
}
