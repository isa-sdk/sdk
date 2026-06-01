package zyins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Reference-data paths under the engine's static-lookup surface.
const (
	referenceStatesPath        = "/v1/reference/states"
	referenceProductTypesPath  = "/v1/reference/product-types"
	referenceNicotineModesPath = "/v1/reference/nicotine-modes"
)

// ReferenceDataService exposes the engine's static lookup tables used
// by client UIs for dropdowns and validation. These payloads are small
// (kilobytes) and change at SDK-release cadence; callers may safely
// memoize results for the process lifetime.
type ReferenceDataService struct {
	client *Client
}

// ReferenceState is one US state in the supported-states table.
type ReferenceState struct {
	// Code is the two-letter postal abbreviation (e.g., "NC").
	Code string `json:"code"`
	// Name is the full state name (e.g., "North Carolina").
	Name string `json:"name"`
	// Supported reports whether the engine accepts prequalify requests
	// for this state; UIs should hide unsupported states or render an
	// explanatory tooltip.
	Supported bool `json:"supported"`
}

// ReferenceProductType is one product category the engine supports.
type ReferenceProductType struct {
	Code        ProductType `json:"code"`
	DisplayName string      `json:"display_name"`
}

// ReferenceNicotineMode is one allowed value for the applicant's
// nicotine-use field.
type ReferenceNicotineMode struct {
	Code        NicotineUsage `json:"code"`
	DisplayName string        `json:"display_name"`
}

// States returns the supported-states table.
func (s *ReferenceDataService) States(ctx context.Context) ([]ReferenceState, error) {
	return getReferenceList[ReferenceState](ctx, s.client, referenceStatesPath)
}

// ProductTypes returns the product-type table.
func (s *ReferenceDataService) ProductTypes(ctx context.Context) ([]ReferenceProductType, error) {
	return getReferenceList[ReferenceProductType](ctx, s.client, referenceProductTypesPath)
}

// NicotineModes returns the nicotine-mode table.
func (s *ReferenceDataService) NicotineModes(ctx context.Context) ([]ReferenceNicotineMode, error) {
	return getReferenceList[ReferenceNicotineMode](ctx, s.client, referenceNicotineModesPath)
}

// getReferenceList is the shared GET-and-decode helper for the three
// reference-data endpoints.
func getReferenceList[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	raw, err := c.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   path,
		op:     "reference_list",
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: reference %s: %w", path, err)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode reference envelope: %w", err)
	}
	target := raw
	if len(env.Data) > 0 {
		target = env.Data
	}
	var out []T
	if err := json.Unmarshal(target, &out); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode reference list: %w", err)
	}
	return out, nil
}
