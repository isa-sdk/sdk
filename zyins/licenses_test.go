package zyins

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureLicensesRequest records the headers and body of one request
// for assertion in tests that exercise the licenses sub-service.
type captureLicensesRequest struct {
	method string
	path   string
	auth   string
	idem   string
	device string
	body   []byte
}

func TestLicenses_Check_HappyPath(t *testing.T) {
	var captured captureLicensesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		captured.idem = r.Header.Get("Idempotency-Key")
		captured.device = r.Header.Get("X-Device-ID")
		captured.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"valid"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.License.Check(context.Background(), &LicenseCheckInput{
		Email:      "john.doe@acme-agency.com",
		Keycode:    "ABC-123-XYZ",
		DeviceID:   " device-1 ",
		LicenseKey: "abc123",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != LicenseStatusValid {
		t.Errorf("status = %q, want valid", result.Status)
	}
	if captured.method != http.MethodPost || captured.path != licensesCheckPath {
		t.Errorf("captured %s %s, want POST %s", captured.method, captured.path, licensesCheckPath)
	}
	// /v2/licenses/* is mounted outside AuthMiddleware on the server;
	// the SDK MUST NOT send an Authorization header for these calls
	// (the chicken-and-egg: activate is what mints the credential).
	if captured.auth != "" {
		t.Errorf("expected no Authorization header on /v2/licenses/check, got %q", captured.auth)
	}
	if captured.idem == "" {
		t.Errorf("expected Idempotency-Key header on POST")
	}
	if captured.device != "device-1" {
		t.Errorf("X-Device-ID = %q, want device-1", captured.device)
	}
	var bodyParsed licensesCheckWireBody
	if err := json.Unmarshal(captured.body, &bodyParsed); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if bodyParsed.Email != "john.doe@acme-agency.com" || bodyParsed.Keycode != "ABC-123-XYZ" {
		t.Errorf("body fields not propagated: %+v", bodyParsed)
	}
	if bodyParsed.DeviceID != "device-1" {
		t.Errorf("body deviceId = %q, want device-1", bodyParsed.DeviceID)
	}
	var rawBody map[string]any
	if err := json.Unmarshal(captured.body, &rawBody); err != nil {
		t.Fatalf("raw body unmarshal: %v", err)
	}
	if _, ok := rawBody["deviceId"]; !ok {
		t.Errorf("expected camelCase deviceId in request body: %s", captured.body)
	}
	if _, ok := rawBody["licenseKey"]; !ok {
		t.Errorf("expected camelCase licenseKey in request body: %s", captured.body)
	}
	if _, ok := rawBody["device_id"]; ok {
		t.Errorf("unexpected legacy device_id in request body: %s", captured.body)
	}
	if _, ok := rawBody["license_key"]; ok {
		t.Errorf("unexpected legacy license_key in request body: %s", captured.body)
	}
}

func TestLicenses_Activate_HappyPath(t *testing.T) {
	var captured captureLicensesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		captured.device = r.Header.Get("X-Device-ID")
		captured.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"active","licenseKey":"lk-v2","remainingActivations":0}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.License.Activate(context.Background(), &LicenseActivateInput{
		Email:    "john.doe@acme-agency.com",
		Keycode:  "ABC-123-XYZ",
		DeviceID: " device-1 ",
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if captured.method != http.MethodPost || captured.path != licensesActivatePath {
		t.Errorf("captured %s %s, want POST %s", captured.method, captured.path, licensesActivatePath)
	}
	if captured.auth != "" {
		t.Errorf("expected no Authorization header on /v2/licenses/activate, got %q", captured.auth)
	}
	if captured.device != "device-1" {
		t.Errorf("X-Device-ID = %q, want device-1", captured.device)
	}
	var rawBody map[string]any
	if err := json.Unmarshal(captured.body, &rawBody); err != nil {
		t.Fatalf("raw body unmarshal: %v", err)
	}
	if _, ok := rawBody["deviceId"]; !ok {
		t.Errorf("expected camelCase deviceId in request body: %s", captured.body)
	}
	if rawBody["deviceId"] != "device-1" {
		t.Errorf("body deviceId = %q, want device-1", rawBody["deviceId"])
	}
	if _, ok := rawBody["device_id"]; ok {
		t.Errorf("unexpected legacy device_id in request body: %s", captured.body)
	}
	if result.LicenseKey != "lk-v2" || result.Auth.LicenseKey != "lk-v2" {
		t.Errorf("license key mirror mismatch: %+v", result)
	}
	if result.RemainingActivations != 0 {
		t.Errorf("remaining activations = %d, want 0", result.RemainingActivations)
	}
}

func TestLicenses_Activate_AcceptsLegacyNestedAuth(t *testing.T) {
	result, err := decodeLicenseActivateResponse([]byte(`{"status":"active","auth":{"license_key":"lk-v1"},"remaining_activations":2}`))
	if err != nil {
		t.Fatalf("decodeLicenseActivateResponse: %v", err)
	}
	if result.LicenseKey != "lk-v1" || result.Auth.LicenseKey != "lk-v1" {
		t.Errorf("legacy license key mismatch: %+v", result)
	}
	if result.RemainingActivations != 2 {
		t.Errorf("remaining activations = %d, want 2", result.RemainingActivations)
	}
}

func TestLicenses_Deactivate_PrefersPresentV2ZeroRemaining(t *testing.T) {
	result, err := decodeLicenseDeactivateResponse([]byte(`{"data":{"status":"inactive","remainingActivations":0,"remaining_activations":3}}`))
	if err != nil {
		t.Fatalf("decodeLicenseDeactivateResponse: %v", err)
	}
	if result.RemainingActivations == nil {
		t.Fatalf("remaining activations = nil, want 0")
	}
	if *result.RemainingActivations != 0 {
		t.Errorf("remaining activations = %d, want 0", *result.RemainingActivations)
	}
}

func TestLicenses_Deactivate_PreservesMissingLegacyRemaining(t *testing.T) {
	result, err := decodeLicenseDeactivateResponse([]byte(`{"status":"deactivated"}`))
	if err != nil {
		t.Fatalf("decodeLicenseDeactivateResponse: %v", err)
	}
	if result.RemainingActivations != nil {
		t.Errorf("remaining activations = %d, want nil", *result.RemainingActivations)
	}
}

func TestLicenses_Check_AcceptsEnvelopedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"inactive"}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.License.Check(context.Background(), &LicenseCheckInput{
		Email: "x@x", Keycode: "ABC-123-XYZ",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != LicenseStatusInactive {
		t.Errorf("status = %q, want inactive", result.Status)
	}
}

func TestLicenses_Check_RejectsMissingEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("request must not hit the wire on validation failure")
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.License.Check(context.Background(), &LicenseCheckInput{Keycode: "ABC-123-XYZ"})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestLicenses_Check_NilInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.License.Check(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected validation error on nil input")
	}
}

func TestLicenses_Check_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"server error","status":500,"code":"server_error"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.License.Check(context.Background(), &LicenseCheckInput{
		Email: "x@x", Keycode: "ABC-123-XYZ",
	})
	if err == nil {
		t.Fatalf("expected error from 500 response")
	}
}

func TestLicenses_Deactivate_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != licensesDeactivatePath {
			t.Errorf("path = %q, want %q", r.URL.Path, licensesDeactivatePath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"deactivated"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.License.Deactivate(context.Background(), &LicenseDeactivateInput{
		Email:    "john.doe@acme-agency.com",
		Keycode:  "ABC-123-XYZ",
		DeviceID: "device-1",
	}, WithIdempotencyKey("550e8400-e29b-41d4-a716-446655440000"))
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if result.Status != "deactivated" {
		t.Errorf("status = %q, want deactivated", result.Status)
	}
}

func TestLicenses_Deactivate_RejectsMissingKeycode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.License.Deactivate(context.Background(), &LicenseDeactivateInput{Email: "x@x"})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}
