package zyins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Dataset paths under the read-only datasets surface.
const (
	datasetsConditionsPath  = "/v1/datasets/conditions"
	datasetsMedicationsPath = "/v1/datasets/medications"
	datasetsBrandsPath      = "/v1/datasets/brands"
	datasetsPlansPath       = "/v1/datasets/plans"
)

// DatasetsService exposes the read-only reference data the engine
// consumes: conditions, medications, brands, plans. Useful for UI
// autocomplete, validation, and offline catalog snapshots.
type DatasetsService struct {
	client *Client
}

// DatasetCondition is one condition row in the conditions dataset.
type DatasetCondition struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Category    string   `json:"category,omitempty"`
	Description string   `json:"description,omitempty"`
}

// DatasetMedication is one drug row in the medications dataset.
type DatasetMedication struct {
	Name        string   `json:"name"`
	Generics    []string `json:"generics,omitempty"`
	Uses        []string `json:"uses,omitempty"`
	Description string   `json:"description,omitempty"`
}

// DatasetBrand is one carrier row in the brands dataset.
type DatasetBrand struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

// DatasetPlan is one plan row in the plans dataset.
type DatasetPlan struct {
	Brand       string `json:"brand"`
	Tier        string `json:"tier"`
	ProductType string `json:"product_type"`
	WireToken   string `json:"wire_token"`
}

// DatasetListOptions paginates a dataset query.
type DatasetListOptions struct {
	// Limit caps the number of returned rows. Zero requests the
	// server's default (typically 100).
	Limit int
	// StartingAfter is the cursor returned by a previous response;
	// empty fetches the first page.
	StartingAfter string
}

// DatasetPage[T] is a paginated dataset response.
type DatasetPage[T any] struct {
	Data    []T    `json:"data"`
	HasMore bool   `json:"has_more"`
	NextID  string `json:"next_id,omitempty"`
}

// Conditions returns one page of condition rows.
func (s *DatasetsService) Conditions(ctx context.Context, opts DatasetListOptions) (*DatasetPage[DatasetCondition], error) {
	return listDataset[DatasetCondition](ctx, s.client, datasetsConditionsPath, opts)
}

// Medications returns one page of medication rows.
func (s *DatasetsService) Medications(ctx context.Context, opts DatasetListOptions) (*DatasetPage[DatasetMedication], error) {
	return listDataset[DatasetMedication](ctx, s.client, datasetsMedicationsPath, opts)
}

// Brands returns one page of brand rows.
func (s *DatasetsService) Brands(ctx context.Context, opts DatasetListOptions) (*DatasetPage[DatasetBrand], error) {
	return listDataset[DatasetBrand](ctx, s.client, datasetsBrandsPath, opts)
}

// Plans returns one page of plan rows.
func (s *DatasetsService) Plans(ctx context.Context, opts DatasetListOptions) (*DatasetPage[DatasetPlan], error) {
	return listDataset[DatasetPlan](ctx, s.client, datasetsPlansPath, opts)
}

// listDataset is the shared GET-with-pagination helper used by every
// dataset reader. Generic over the row type so the four public methods
// above are one-liners.
func listDataset[T any](ctx context.Context, c *Client, path string, opts DatasetListOptions) (*DatasetPage[T], error) {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.StartingAfter != "" {
		q.Set("starting_after", opts.StartingAfter)
	}
	full := path
	if encoded := q.Encode(); encoded != "" {
		full = path + "?" + encoded
	}
	raw, err := c.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   full,
		op:     "datasets_list",
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: datasets %s: %w", path, err)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode dataset envelope: %w", err)
	}
	target := raw
	if len(env.Data) > 0 {
		// An explicit `null` payload is len > 0 (`"null"`) but represents
		// "no data". Reject it rather than unmarshaling silently into a
		// zero-value page — a caller that asked for conditions and got
		// `{"data":null}` has a real server-side bug to surface, not a
		// zero-row page to render.
		if isJSONNull(env.Data) {
			return nil, fmt.Errorf("zyins: dataset %s returned null data envelope", path)
		}
		target = env.Data
	}
	var page DatasetPage[T]
	if err := json.Unmarshal(target, &page); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode dataset page: %w", err)
	}
	return &page, nil
}

// isJSONNull reports whether raw is the literal JSON `null` token,
// ignoring surrounding whitespace the encoder may emit.
func isJSONNull(raw json.RawMessage) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case 'n':
			return string(bytes.TrimSpace(raw)) == "null"
		default:
			return false
		}
	}
	return false
}
