package account

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/isa-sdk/sdk/core/license"
)

// fakeDoer is a deterministic HTTP stub. It records the last request
// and returns the configured response.
type fakeDoer struct {
	last     *http.Request
	lastBody []byte
	resp     *http.Response
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.last = req
	if req.Body != nil {
		f.lastBody, _ = io.ReadAll(req.Body)
	}
	return f.resp, nil
}

func newClient(t *testing.T, body string, status int) (*Client, *fakeDoer) {
	t.Helper()
	doer := &fakeDoer{
		resp: &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}
	fixedClock := func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	c, err := NewClient(
		Auth{LicenseKey: "lk", OrderID: "ABC-123-XYZ", Email: "agent@example.com", DeviceID: "dev"},
		WithHTTPClient(doer),
		WithClock(license.Clock(fixedClock)),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, doer
}

func TestNewClient_RequiresAllAuthFields(t *testing.T) {
	if _, err := NewClient(Auth{}); err == nil {
		t.Fatal("expected error for empty Auth")
	}
}

func TestNewClient_RejectsWhitespaceOnlyAuthFields(t *testing.T) {
	if _, err := NewClient(Auth{LicenseKey: " ", OrderID: "ABC-123-XYZ", Email: "agent@example.com", DeviceID: "dev"}); err == nil {
		t.Fatal("expected error for whitespace-only LicenseKey")
	}
}

func TestBranding_Lookup_EmptyBody(t *testing.T) {
	c, _ := newClient(t, "", 200)
	out, err := c.Branding.Lookup(context.Background(), nil)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if out.IMOName != "" {
		t.Errorf("expected zero-value IMOName, got %q", out.IMOName)
	}
}

func TestBranding_Lookup_SignsAndDecodes(t *testing.T) {
	c, doer := newClient(t, `{"data":{"imo_name":"Acme","main_color":"#abc"}}`, 200)
	out, err := c.Branding.Lookup(context.Background(), nil)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if out.IMOName != "Acme" {
		t.Errorf("IMOName=%q", out.IMOName)
	}
	if out.PrimaryColor != "#abc" {
		t.Errorf("PrimaryColor fallback to main_color failed: %q", out.PrimaryColor)
	}
	if doer.last.Header.Get("Authorization") == "" {
		t.Error("Authorization header missing")
	}
	if doer.last.Header.Get("X-Device-Signature") == "" {
		t.Error("X-Device-Signature header missing")
	}
}

func TestPreferences_LookupAndSet(t *testing.T) {
	c, doer := newClient(t, `{"prefs":{"theme":"dark"}}`, 200)
	prefs, err := c.Preferences.Lookup(context.Background(), "bpp")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if prefs["theme"] != "dark" {
		t.Errorf("prefs[theme]=%v", prefs["theme"])
	}
	if !strings.Contains(doer.last.URL.RawQuery, "scope=bpp") {
		t.Errorf("expected scope=bpp in query, got %q", doer.last.URL.RawQuery)
	}
	doer.resp = &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}
	ok, err := c.Preferences.Set(context.Background(), "bpp", PreferencesDocument{"theme": "light"})
	if err != nil || !ok {
		t.Fatalf("Set: ok=%v err=%v", ok, err)
	}
	var wire map[string]any
	_ = json.Unmarshal(doer.lastBody, &wire)
	if wire["scope"] != "bpp" {
		t.Errorf("wire.scope=%v", wire["scope"])
	}
	if doer.last.Header.Get("Idempotency-Key") == "" {
		t.Error("expected Idempotency-Key header on Set")
	}
}

func TestCases_Create_RoundTrip(t *testing.T) {
	c, doer := newClient(t,
		`{"hash":"abc","url":"https://q.zyins/c/abc","readonly":false,"created_at":"2026-05-21T00:00:00Z"}`,
		200,
	)
	out, err := c.Cases.Create(context.Background(), CaseCreateInput{
		Input:    map[string]any{"age": 60},
		Products: []string{"x", "y"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Hash != "abc" {
		t.Errorf("Hash=%q", out.Hash)
	}
	if !bytes.Contains(doer.lastBody, []byte(`"products":["x","y"]`)) {
		t.Errorf("body missing products: %s", doer.lastBody)
	}
}

func TestEmail_Enqueue(t *testing.T) {
	c, _ := newClient(t, `{"status":"queued"}`, 200)
	ok, err := c.Email.Enqueue(context.Background(), EmailEnqueueInput{
		To: []string{"a@b.co"}, Subject: "s", Body: "b",
	})
	if err != nil || !ok {
		t.Fatalf("Enqueue: ok=%v err=%v", ok, err)
	}
}

func TestReferenceData_DispatchByScope(t *testing.T) {
	c, doer := newClient(t, `{"datasets":{"states":[]}}`, 200)
	out, err := c.ReferenceData.Get(context.Background(), "dataset", WithDataset("brands"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doer.last.URL.Path != "/dataset/brands" {
		t.Errorf("expected /dataset/brands, got %q", doer.last.URL.Path)
	}
	if _, ok := out["datasets"]; !ok {
		t.Errorf("expected datasets key, got %v", out)
	}

	c.ReferenceData.Get(context.Background(), "compiled_data_v3", WithPayload(map[string]any{"x": 1}))
	if doer.last.URL.Path != "/v2/reference-data" {
		t.Errorf("expected /v2/reference-data for compiled_data_v3, got %q", doer.last.URL.Path)
	}
}

func TestReferenceData_PayloadCannotOverrideScope(t *testing.T) {
	c, doer := newClient(t, `{"ok":true}`, 200)
	_, err := c.ReferenceData.Get(context.Background(), "compiled_data_v3", WithPayload(map[string]any{
		"scope": "wrong",
		"x":     1,
	}))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var wire map[string]any
	_ = json.Unmarshal(doer.lastBody, &wire)
	if wire["scope"] != "compiled_data_v3" {
		t.Errorf("wire.scope=%v", wire["scope"])
	}
}
