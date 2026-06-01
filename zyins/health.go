package zyins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// readinessPath is the public no-auth readiness probe (see ADR-021 +
// shared/schemas/api/isa/v1/health.proto).
const readinessPath = "/ready"

// HealthService exposes the platform health/readiness contract. Both
// /health and /ready are unauthenticated; an attached bearer token is
// ignored server-side. Liveness (/health) is reserved for a follow-up
// PR — readiness is the first surfaced operation because it is the
// signal load balancers and runbooks rely on.
type HealthService struct {
	client *Client
}

// ServingStatus mirrors the proto `ServingStatus` enum. Wire values
// are lower-case strings.
type ServingStatus string

// Enumerated serving states.
const (
	ServingStatusServing    ServingStatus = "serving"
	ServingStatusNotServing ServingStatus = "not_serving"
	ServingStatusUnknown    ServingStatus = "unknown"
)

// ProbeResult is the per-dependency readiness outcome carried inside
// every readiness response.
type ProbeResult struct {
	// Status is the current serving state of this dependency.
	Status ServingStatus `json:"status"`
	// LatencyMs is the observed round-trip latency in milliseconds.
	// Zero when the probe could not complete.
	LatencyMs int64 `json:"latency_ms"`
	// Message is a human-readable explanation when Status is not
	// "serving"; empty otherwise.
	Message string `json:"message,omitempty"`
	// CheckedAt is the wall-clock time at which this probe ran.
	CheckedAt time.Time `json:"checked_at"`
}

func (p *ProbeResult) UnmarshalJSON(data []byte) error {
	var wire struct {
		Status    ServingStatus   `json:"status"`
		LatencyMs json.RawMessage `json:"latency_ms"`
		Message   string          `json:"message,omitempty"`
		CheckedAt time.Time       `json:"checked_at"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	latencyMs, err := parseLatencyMs(wire.LatencyMs)
	if err != nil {
		return err
	}
	p.Status = wire.Status
	p.LatencyMs = latencyMs
	p.Message = wire.Message
	p.CheckedAt = wire.CheckedAt
	return nil
}

func parseLatencyMs(raw json.RawMessage) (int64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return 0, nil
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("zyins: invalid readiness latency_ms %q: %w", text, err)
		}
		return n, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var n json.Number
	if err := dec.Decode(&n); err == nil {
		i, err := n.Int64()
		if err != nil {
			return 0, fmt.Errorf("zyins: invalid readiness latency_ms %q: %w", n.String(), err)
		}
		return i, nil
	}
	return 0, fmt.Errorf("zyins: invalid readiness latency_ms %s", string(raw))
}

// ReadinessResult is the typed response shape for Health.GetReadiness.
type ReadinessResult struct {
	// Ready is true iff every required sub-probe returned "serving".
	Ready bool `json:"ready"`
	// Status mirrors Ready using the shared enum.
	Status ServingStatus `json:"status"`
	// DB is the primary dependency probe (database pool for ZyINS).
	DB ProbeResult `json:"db"`
	// Cache is the secondary dependency probe.
	Cache ProbeResult `json:"cache"`
	// DownstreamServices keys additional downstream probes by logical
	// service name (e.g., "accounts", "billing", "eapp"). Order is not
	// significant.
	DownstreamServices map[string]ProbeResult `json:"downstream_services,omitempty"`
	// CheckedAt is the wall-clock time at which this readiness
	// evaluation ran.
	CheckedAt time.Time `json:"checked_at"`
}

// GetReadiness queries /ready and returns the typed readiness result.
// A 503 response surfaces as an *Error with Code service_unavailable;
// callers that probe non-fatally should branch on ErrorCodeServiceDown.
func (s *HealthService) GetReadiness(ctx context.Context) (*ReadinessResult, error) {
	raw, err := s.client.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   readinessPath,
		op:     "readiness",
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Health.GetReadiness: %w", err)
	}
	data, err := unwrapEnvelope(raw, "readiness")
	if err != nil {
		return nil, err
	}
	var result ReadinessResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode readiness response: %w", err)
	}
	return &result, nil
}
