// Package zyins — v3-pinned routing tests for Prequalify and Quote.
//
// When the client is pinned to v3 on a surface, the v3 service must hit
// the /v3/{prequalify,quote} path; the legacy v1/v2 entrypoint must
// reject the call with *ConfigError pointing to the v3 entrypoint
// (their input types differ structurally).
//
// Mirrors the TS "v3 facade routing" suite added in PR #377.

package zyins

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// V3 happy path — pinned to v3, request hits /v3/prequalify and /v3/quote.
// ---------------------------------------------------------------------------

func TestPrequalifyV3_Run_HitsV3PathWhenPinned(t *testing.T) {
	body := `{
		"object":"prequalify_result",
		"request_id":"req_01HZK2N5GQR9T8X4B6FJW3Y1AS",
		"idempotency_key":"550e8400-e29b-41d4-a716-446655440000",
		"livemode":true,
		"data":{"plans":[]}
	}`
	srv, captured := newRoutingServer(t, body)
	c := newRoutingClient(t, srv, map[string]string{"prequalify": "v3"})

	result, err := c.PrequalifyV3.Run(context.Background(), &PrequalifyV3Request{
		Applicant: routingApplicant(t),
		Coverage:  routingCoverage(t),
		Products:  routingProducts(t),
	})
	if err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}
	if captured.path != "/v3/prequalify" {
		t.Errorf("path = %q, want /v3/prequalify", captured.path)
	}
	if captured.method != http.MethodPost {
		t.Errorf("method = %q, want POST", captured.method)
	}
	if result.RequestID != "req_01HZK2N5GQR9T8X4B6FJW3Y1AS" {
		t.Errorf("RequestID = %q, want req_01HZK2N5GQR9T8X4B6FJW3Y1AS", result.RequestID)
	}
	if !result.Livemode {
		t.Error("Livemode = false, want true")
	}
}

func TestQuoteV3_Run_HitsV3PathWhenPinned(t *testing.T) {
	body := `{
		"object":"quote_result",
		"request_id":"req_01HZK2N5GQR9T8X4B6FJW3Y1AS",
		"idempotency_key":"550e8400-e29b-41d4-a716-446655440000",
		"livemode":true,
		"data":{"plans":[]}
	}`
	srv, captured := newRoutingServer(t, body)
	c := newRoutingClient(t, srv, map[string]string{"quote": "v3"})

	result, err := c.QuoteV3.Run(context.Background(), &QuoteV3Request{
		Applicant: routingApplicant(t),
		Coverage:  routingCoverage(t),
		Products:  routingProducts(t),
	})
	if err != nil {
		t.Fatalf("QuoteV3.Run: %v", err)
	}
	if captured.path != "/v3/quote" {
		t.Errorf("path = %q, want /v3/quote", captured.path)
	}
	if result.RequestID != "req_01HZK2N5GQR9T8X4B6FJW3Y1AS" {
		t.Errorf("RequestID = %q, want req_01HZK2N5GQR9T8X4B6FJW3Y1AS", result.RequestID)
	}
}

// ---------------------------------------------------------------------------
// Inverse guard — pinning to v3 fails the legacy v1/v2 entrypoints. The
// caller must use PrequalifyV3 / QuoteV3 with the v3 request shape; the
// input types differ structurally so silent dispatch would yield a
// misleading wire body.
// ---------------------------------------------------------------------------

func TestPrequalifyService_Run_RejectsCallWhenPinnedToV3(t *testing.T) {
	srv, captured := newRoutingServer(t, `{"data":{"plans":[]}}`)
	c := newRoutingClient(t, srv, map[string]string{"prequalify": "v3"})

	_, err := c.Prequalify.Run(context.Background(), &PrequalifyInput{
		Applicant: routingApplicant(t),
		Coverage:  routingCoverage(t),
		Products:  routingProducts(t),
	})
	if err == nil {
		t.Fatal("Prequalify.Run should fail when client is pinned to v3")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("want *ConfigError, got %T: %v", err, err)
	}
	if cfgErr.Factory != "Prequalify.Run" {
		t.Errorf("Factory = %q, want Prequalify.Run", cfgErr.Factory)
	}
	if !strings.Contains(cfgErr.Detail, "PrequalifyV3.Run") {
		t.Errorf("Detail should redirect to PrequalifyV3.Run; got %q", cfgErr.Detail)
	}
	if captured.path != "" {
		t.Errorf("server should not have been hit; got path %q", captured.path)
	}
}

func TestQuoteService_Run_RejectsCallWhenPinnedToV3(t *testing.T) {
	srv, captured := newRoutingServer(t, `{"data":{"plans":[]}}`)
	c := newRoutingClient(t, srv, map[string]string{"quote": "v3"})

	_, err := c.Quote.Run(context.Background(), &QuoteInput{
		Applicant:    routingApplicant(t),
		Coverage:     routingCoverage(t),
		ProductToken: routingProductWireID,
	})
	if err == nil {
		t.Fatal("Quote.Run should fail when client is pinned to v3")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("want *ConfigError, got %T: %v", err, err)
	}
	if cfgErr.Factory != "Quote.Run" {
		t.Errorf("Factory = %q, want Quote.Run", cfgErr.Factory)
	}
	if !strings.Contains(cfgErr.Detail, "QuoteV3.Run") {
		t.Errorf("Detail should redirect to QuoteV3.Run; got %q", cfgErr.Detail)
	}
	if captured.path != "" {
		t.Errorf("server should not have been hit; got path %q", captured.path)
	}
}
