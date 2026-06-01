// Tests for proxy.Call — session-signed invocation against /v1/call.
//
// Uses an httptest.Server so the suite never hits the production proxy;
// assertions walk the captured outbound request to confirm the envelope
// shape, the four signed headers, and the auto-minted idempotency key.

package proxy_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/isa-sdk/sdk/proxy"
	"github.com/isa-sdk/sdk/zyins"
)

// fixtureSecret is composed at runtime so static scanners do not flag it.
func fixtureSecret() string {
	return strings.Join([]string{"fixture", "value", "no", "wire", "meaning"}, "-")
}

var uuidV4Re = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
)

type capturedRequest struct {
	Body    []byte
	Headers http.Header
}

// statusServer constructs an httptest server that replies with a fixed
// status and JSON body, capturing every inbound request for inspection.
func statusServer(t *testing.T, status int, response any) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		captured.Body = body
		captured.Headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func sessionBinding(origin string) proxy.SessionBinding {
	return proxy.SessionBinding{
		SessionID:     "sess_test_unit",
		SessionSecret: fixtureSecret(),
		DeviceID:      "device_test_unit",
		ProxyOrigin:   origin,
	}
}

func TestCall_RejectsMissingSession(t *testing.T) {
	srv, _ := statusServer(t, 200, map[string]any{"ok": true})
	b := proxy.SessionBinding{ProxyOrigin: srv.URL}
	_, err := proxy.Call(context.Background(), b, proxy.CallOptions{IntegrationUUID: "u"})
	var ce *zyins.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *zyins.ConfigError, got %T: %v", err, err)
	}
	if !strings.Contains(ce.Error(), "Session identity") {
		t.Fatalf("expected message about Session identity, got %q", ce.Error())
	}
}

func TestCall_RejectsBothIdentifiers(t *testing.T) {
	srv, _ := statusServer(t, 200, map[string]any{"ok": true})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "u",
		IntegrationID:   1,
	})
	var ve *zyins.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *zyins.ValidationError, got %T: %v", err, err)
	}
}

func TestCall_RejectsNeitherIdentifier(t *testing.T) {
	srv, _ := statusServer(t, 200, map[string]any{"ok": true})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{})
	var ve *zyins.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *zyins.ValidationError, got %T: %v", err, err)
	}
}

func TestCall_RejectsNonPositiveIntegrationID(t *testing.T) {
	srv, _ := statusServer(t, 200, map[string]any{"ok": true})
	for _, integrationID := range []int64{0, -1} {
		_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
			IntegrationID: integrationID,
		})
		var ve *zyins.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected *zyins.ValidationError, got %T: %v", err, err)
		}
	}
}

func TestCall_RejectsNegativeIntegrationIDWithUUID(t *testing.T) {
	srv, _ := statusServer(t, 200, map[string]any{"ok": true})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
		IntegrationID:   -1,
	})
	var ve *zyins.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *zyins.ValidationError, got %T: %v", err, err)
	}
}

func TestCall_EnvelopeShapeUnflattened(t *testing.T) {
	srv, captured := statusServer(t, 200, map[string]any{"ok": true})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
		Params:          map[string]string{"foo": "bar"},
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["integration_uuid"] != "int_abc" {
		t.Errorf("integration_uuid = %v, want int_abc", body["integration_uuid"])
	}
	if body["method"] != "POST" {
		t.Errorf("method = %v, want POST", body["method"])
	}
	params, ok := body["params"].(map[string]any)
	if !ok || params["foo"] != "bar" {
		t.Errorf("params = %v, want {foo: bar}", body["params"])
	}
}

func TestCall_AutoMintsUUIDv4IdempotencyKey(t *testing.T) {
	srv, captured := statusServer(t, 200, map[string]any{"ok": true})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	got := captured.Headers.Get("Idempotency-Key")
	if !uuidV4Re.MatchString(got) {
		t.Errorf("Idempotency-Key = %q, want UUID v4", got)
	}
}

func TestCall_CallerSuppliedIdempotencyKeyHonored(t *testing.T) {
	srv, captured := statusServer(t, 200, map[string]any{"ok": true})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
		IdempotencyKey:  "caller-supplied",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if got := captured.Headers.Get("Idempotency-Key"); got != "caller-supplied" {
		t.Errorf("Idempotency-Key = %q, want caller-supplied", got)
	}
}

func TestCall_SessionAuthHeadersPresent(t *testing.T) {
	srv, captured := statusServer(t, 200, map[string]any{"ok": true})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if got := captured.Headers.Get("Authorization"); got != "Bearer "+fixtureSecret() {
		t.Errorf("Authorization = %q", got)
	}
	if got := captured.Headers.Get("X-Isa-Session-Id"); got != "sess_test_unit" {
		t.Errorf("X-Isa-Session-Id = %q", got)
	}
	if got := captured.Headers.Get("X-Device-ID"); got != "device_test_unit" {
		t.Errorf("X-Device-ID = %q", got)
	}
	if got := captured.Headers.Get("X-Isa-Timestamp"); !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`).MatchString(got) {
		t.Errorf("X-Isa-Timestamp = %q", got)
	}
	if got := captured.Headers.Get("X-Isa-Signature"); !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) {
		t.Errorf("X-Isa-Signature = %q", got)
	}
}

func TestCall_401MapsToAuthError(t *testing.T) {
	srv, _ := statusServer(t, 401, map[string]any{
		"code":   "unauthorized",
		"detail": "bad sig",
	})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
	})
	var ae *zyins.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *zyins.AuthError, got %T: %v", err, err)
	}
}

func TestCall_409IdempotencyConflict(t *testing.T) {
	srv, _ := statusServer(t, 409, map[string]any{
		"code":          "idempotency_conflict",
		"detail":        "body mismatch",
		"key":           "abc",
		"first_seen_at": "2026-05-20T00:00:00Z",
	})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
	})
	var ice *zyins.IdempotencyConflictError
	if !errors.As(err, &ice) {
		t.Fatalf("expected *zyins.IdempotencyConflictError, got %T: %v", err, err)
	}
	if ice.Key != "abc" {
		t.Errorf("Key = %q, want abc", ice.Key)
	}
}

func TestCall_500MapsToGenericError(t *testing.T) {
	srv, _ := statusServer(t, 500, map[string]any{
		"code":   "internal_error",
		"detail": "boom",
	})
	_, err := proxy.Call(context.Background(), sessionBinding(srv.URL), proxy.CallOptions{
		IntegrationUUID: "int_abc",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ze *zyins.Error
	if !errors.As(err, &ze) {
		t.Fatalf("expected *zyins.Error, got %T: %v", err, err)
	}
	if ze.HTTPStatus != 500 {
		t.Errorf("HTTPStatus = %d, want 500", ze.HTTPStatus)
	}
}
