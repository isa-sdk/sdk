// Package zyins — v3 mispin and default-behavior tests for facade routing.
//
// Verifies the guards reject v3 entrypoints when the client is not
// pinned to v3, and that the default (no override) keeps the existing
// behavior of the legacy Prequalify / Quote services intact.

package zyins

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// PrequalifyV3 guard — fires when client is NOT pinned to v3 on prequalify.
// ---------------------------------------------------------------------------

func TestPrequalifyV3_Run_RejectsCallWhenNotPinnedToV3(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]string
	}{
		{"bundled default (v2)", nil},
		{"explicitly pinned to v1", map[string]string{"prequalify": "v1"}},
		{"pinned to v2 (no-op)", map[string]string{"prequalify": "v2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, captured := newRoutingServer(t, `{"data":{"plans":[]}}`)
			c := newRoutingClient(t, srv, tc.overrides)

			_, err := c.PrequalifyV3.Run(context.Background(), &PrequalifyV3Request{
				Applicant: routingApplicant(t),
				Coverage:  routingCoverage(t),
				Products:  routingProducts(t),
			})
			if err == nil {
				t.Fatal("PrequalifyV3.Run should fail when not pinned to v3")
			}
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("want *ConfigError, got %T: %v", err, err)
			}
			if cfgErr.Factory != "PrequalifyV3.Run" {
				t.Errorf("Factory = %q, want PrequalifyV3.Run", cfgErr.Factory)
			}
			if !strings.Contains(cfgErr.Detail, "v3") {
				t.Errorf("Detail missing v3 mention: %q", cfgErr.Detail)
			}
			if !strings.Contains(cfgErr.Detail, "prequalify") {
				t.Errorf("Detail missing prequalify mention: %q", cfgErr.Detail)
			}
			if captured.path != "" {
				t.Errorf("server should not have been hit; got path %q", captured.path)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// QuoteV3 guard — fires when client is NOT pinned to v3 on quote.
// ---------------------------------------------------------------------------

func TestQuoteV3_Run_RejectsCallWhenNotPinnedToV3(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]string
	}{
		{"bundled default (v2)", nil},
		{"explicitly pinned to v1", map[string]string{"quote": "v1"}},
		{"pinned to v2 (no-op)", map[string]string{"quote": "v2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, captured := newRoutingServer(t, `{"data":{"plans":[]}}`)
			c := newRoutingClient(t, srv, tc.overrides)

			_, err := c.QuoteV3.Run(context.Background(), &QuoteV3Request{
				Applicant: routingApplicant(t),
				Coverage:  routingCoverage(t),
				Products:  routingProducts(t),
			})
			if err == nil {
				t.Fatal("QuoteV3.Run should fail when not pinned to v3")
			}
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("want *ConfigError, got %T: %v", err, err)
			}
			if cfgErr.Factory != "QuoteV3.Run" {
				t.Errorf("Factory = %q, want QuoteV3.Run", cfgErr.Factory)
			}
			if !strings.Contains(cfgErr.Detail, "quote") {
				t.Errorf("Detail missing quote mention: %q", cfgErr.Detail)
			}
			if captured.path != "" {
				t.Errorf("server should not have been hit; got path %q", captured.path)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Default behavior preserved — no override leaves Prequalify / Quote on
// their existing wire path. This regression-guards the known drift
// called out in #364's PR description: BundledAPIVersions says v2 but
// the path constant still points at /v1/... until the per-surface URL
// reconciliation PR lands. We assert the CURRENT behavior so the
// future reconciliation has to update this test alongside the code.
// ---------------------------------------------------------------------------

func TestPrequalifyService_Run_BundledDefaultHitsLegacyV1Path(t *testing.T) {
	body := `{"data":{"plans":[]},"request_id":"req_legacy"}`
	srv, captured := newRoutingServer(t, body)
	c := newRoutingClient(t, srv, nil)

	result, err := c.Prequalify.Run(context.Background(), &PrequalifyInput{
		Applicant: routingApplicant(t),
		Coverage:  routingCoverage(t),
		Products:  routingProducts(t),
	})
	if err != nil {
		t.Fatalf("Prequalify.Run: %v", err)
	}
	if captured.path != "/v1/prequalify" {
		t.Errorf("path = %q, want /v1/prequalify (current behavior; per-surface URL reconciliation is a follow-up)", captured.path)
	}
	if result.RequestID != "req_legacy" {
		t.Errorf("RequestID = %q, want req_legacy", result.RequestID)
	}
}

func TestQuoteService_Run_BundledDefaultHitsLegacyV1Path(t *testing.T) {
	body := `{"data":{"quote_id":"q_x","monthly_premium_cents":1000,"face_value_cents":100000,"request_id":"req_legacy"},"request_id":"req_legacy"}`
	srv, captured := newRoutingServer(t, body)
	c := newRoutingClient(t, srv, nil)

	_, err := c.Quote.Run(context.Background(), &QuoteInput{
		Applicant:    routingApplicant(t),
		Coverage:     routingCoverage(t),
		ProductToken: routingProductWireID,
	})
	if err != nil {
		t.Fatalf("Quote.Run: %v", err)
	}
	if captured.path != "/v1/quote" {
		t.Errorf("path = %q, want /v1/quote", captured.path)
	}
}

// ---------------------------------------------------------------------------
// APIVersionFor — verifies the resolver agrees with the routing guards
// (defensive against drift between BundledAPIVersions and the routing
// implementation).
// ---------------------------------------------------------------------------

func TestRouting_APIVersionFor_PrequalifyAndQuote(t *testing.T) {
	cases := []struct {
		name           string
		overrides      map[string]string
		wantPrequalify string
		wantQuote      string
	}{
		{"defaults", nil, "v2", "v2"},
		{"prequalify v3", map[string]string{"prequalify": "v3"}, "v3", "v2"},
		{"quote v3", map[string]string{"quote": "v3"}, "v2", "v3"},
		{"both v3", map[string]string{"prequalify": "v3", "quote": "v3"}, "v3", "v3"},
		{"prequalify v1", map[string]string{"prequalify": "v1"}, "v1", "v2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newRoutingServer(t, "{}")
			c := newRoutingClient(t, srv, tc.overrides)

			if got := c.APIVersionFor("prequalify"); got != tc.wantPrequalify {
				t.Errorf("APIVersionFor(prequalify) = %q, want %q", got, tc.wantPrequalify)
			}
			if got := c.APIVersionFor("quote"); got != tc.wantQuote {
				t.Errorf("APIVersionFor(quote) = %q, want %q", got, tc.wantQuote)
			}
		})
	}
}
