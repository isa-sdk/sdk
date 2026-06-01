package zyins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// usageCurrentPath is the canonical path for the usage operation.
const usageCurrentPath = "/v1/usage/current"

// UsageService reads consumption and quota counters for the token's
// account. Callers surface this to operators (dashboard widgets,
// approaching-cap alerts).
type UsageService struct {
	client *Client
}

// UsageSnapshot is the typed response shape for Usage.Current.
type UsageSnapshot struct {
	// PeriodStart is the start of the current billing period as an
	// ISO 8601 timestamp.
	PeriodStart string `json:"period_start"`
	// PeriodEnd is the end of the current billing period.
	PeriodEnd string `json:"period_end"`
	// PrequalifyCount is the number of prequalify calls executed in
	// the current period.
	PrequalifyCount int64 `json:"prequalify_count"`
	// QuoteCount is the number of quote calls executed in the current
	// period.
	QuoteCount int64 `json:"quote_count"`
	// QuotaLimit caps the combined operation count for the period;
	// zero means unlimited.
	QuotaLimit int64 `json:"quota_limit"`
	// RequestID is the server-side request correlator.
	RequestID string `json:"request_id"`
}

// Current returns the consumption counters for the current billing
// period.
func (s *UsageService) Current(ctx context.Context) (*UsageSnapshot, error) {
	raw, err := s.client.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   usageCurrentPath,
		op:     "usage_current",
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: usage.Current: %w", err)
	}
	var env struct {
		Data      json.RawMessage `json:"data"`
		RequestID string          `json:"request_id"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode usage envelope: %w", err)
	}
	target := raw
	if len(env.Data) > 0 {
		target = env.Data
	}
	var snap UsageSnapshot
	if err := json.Unmarshal(target, &snap); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode usage snapshot: %w", err)
	}
	if snap.RequestID == "" && env.RequestID != "" {
		snap.RequestID = env.RequestID
	}
	return &snap, nil
}
