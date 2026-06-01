// Package zyins — shared test helpers for the v3 facade routing suite.
//
// Centralized so the v3-pinned-happy-path tests and the
// mispin-rejection tests can share one client builder, one canned
// applicant, and one captured-server helper. Splitting the routing tests
// across multiple files keeps each focused under the 250-line ceiling.

package zyins

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// routingProductWireID is the product wire identifier used by every
// routing test. Declared once so a future rename only touches one
// constant.
const routingProductWireID = "aetna-test-product"

// routingCapturedRequest records what the server actually saw so the
// test can assert the routing chose the right /vN/... path.
type routingCapturedRequest struct {
	method string
	path   string
}

// newRoutingServer stands up an httptest server that returns the
// supplied body with HTTP 200 and records the request path. Cleanup is
// registered on t so callers never have to defer Close.
func newRoutingServer(t *testing.T, body string) (*httptest.Server, *routingCapturedRequest) {
	t.Helper()
	captured := &routingCapturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		// Drain so the server is reusable across retries.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// newRoutingClient builds a client wired to srv with the supplied
// per-surface APIVersion overrides. A nil overrides map means "use the
// BundledAPIVersions defaults" (prequalify=v2, quote=v2).
func newRoutingClient(t *testing.T, srv *httptest.Server, overrides map[string]string) *Client {
	t.Helper()
	opts := []Option{
		WithToken("isa_test_4fjK2nQ7mX1aB8sR9pZ3"),
		WithBaseURL(srv.URL),
	}
	if overrides != nil {
		opts = append(opts, WithAPIVersionOverrides(overrides))
	}
	c, err := NewClient(opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// routingApplicant returns the canonical applicant used by every routing
// test. Centralizing the construction means input drift never fails a
// routing test for the wrong reason.
func routingApplicant(t *testing.T) Applicant {
	t.Helper()
	h, err := NewHeight(5, 10)
	if err != nil {
		t.Fatalf("NewHeight: %v", err)
	}
	w, err := NewWeight(195)
	if err != nil {
		t.Fatalf("NewWeight: %v", err)
	}
	return Applicant{
		DOB:         "1962-04-18",
		Sex:         SexMale,
		Height:      h,
		Weight:      w,
		State:       "NC",
		NicotineUse: NicotineUsageInput{LastUsed: NicotineNever},
	}
}

func routingProducts(t *testing.T) ProductSelection {
	t.Helper()
	ps, err := NewProductSelection(routingProductWireID)
	if err != nil {
		t.Fatalf("NewProductSelection: %v", err)
	}
	return ps
}

func routingCoverage(t *testing.T) Coverage {
	t.Helper()
	cov, err := NewFaceValueCoverage(25000)
	if err != nil {
		t.Fatalf("NewFaceValueCoverage: %v", err)
	}
	return cov
}
