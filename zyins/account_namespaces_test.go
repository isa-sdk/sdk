package zyins

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type captureAcctRequest struct {
	method string
	path   string
	idem   string
	body   []byte
}

func newServerCapturing(t *testing.T, status int, response string) (*httptest.Server, *captureAcctRequest) {
	t.Helper()
	cap := &captureAcctRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.idem = r.Header.Get("Idempotency-Key")
		cap.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	return srv, cap
}

// ---------------------- Branding -----------------------------------

func TestBranding_Lookup_HappyPath(t *testing.T) {
	resp := `{"imo_name":"Acme Agency","imo_logo":"https://cdn.example/logo.png","hide_affiliate_leads":"true","prevent_product_selection":false,"nav_color":"#111"}`
	srv, cap := newServerCapturing(t, http.StatusOK, resp)
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Branding.Lookup(context.Background())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.IMOName != "Acme Agency" {
		t.Errorf("IMOName=%q, want Acme Agency", result.IMOName)
	}
	if !result.HideAffiliateLeads {
		t.Errorf("HideAffiliateLeads=false, want true (string-true)")
	}
	if result.PreventProductSelection {
		t.Errorf("PreventProductSelection=true, want false")
	}
	if cap.method != http.MethodGet || cap.path != brandingLookupPath {
		t.Errorf("captured %s %s, want GET %s", cap.method, cap.path, brandingLookupPath)
	}
}

func TestBranding_Lookup_AcceptsEnvelope(t *testing.T) {
	srv, _ := newServerCapturing(t, http.StatusOK, `{"data":{"imo_name":"Wrapped Co"}}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Branding.Lookup(context.Background())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.IMOName != "Wrapped Co" {
		t.Errorf("IMOName=%q, want Wrapped Co", result.IMOName)
	}
}

func TestBranding_Lookup_500Errors(t *testing.T) {
	body := `{"type":"about:blank","title":"server","status":500,"code":"server_error"}`
	srv, _ := newServerCapturing(t, http.StatusInternalServerError, body)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Branding.Lookup(context.Background())
	if err == nil {
		t.Fatalf("expected error on 500")
	}
}

// ---------------------- Preferences -------------------------------

func TestPreferences_Lookup_ReturnsPrefs(t *testing.T) {
	srv, cap := newServerCapturing(t, http.StatusOK, `{"prefs":{"theme":"dark"}}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Preferences.Lookup(context.Background())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.Prefs["theme"] != "dark" {
		t.Errorf("prefs.theme=%v, want dark", result.Prefs["theme"])
	}
	if cap.method != http.MethodGet || cap.path != preferencesPath {
		t.Errorf("captured %s %s, want GET %s", cap.method, cap.path, preferencesPath)
	}
}

func TestPreferences_Set_SerializesAndMintsIdempotencyKey(t *testing.T) {
	srv, cap := newServerCapturing(t, http.StatusOK, `{"prefs":{"theme":"dark"}}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Preferences.Set(context.Background(), &PreferencesSetInput{Prefs: PreferencesDocument{"theme": "dark"}})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cap.idem == "" {
		t.Errorf("expected Idempotency-Key header on POST")
	}
	var wire prefsWireBody
	if err := json.Unmarshal(cap.body, &wire); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if wire.Prefs["theme"] != "dark" {
		t.Errorf("prefs.theme=%v, want dark", wire.Prefs["theme"])
	}
}

func TestPreferences_Set_EmptyBodyFallsBackToRequest(t *testing.T) {
	srv, _ := newServerCapturing(t, http.StatusOK, "")
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Preferences.Set(context.Background(), &PreferencesSetInput{Prefs: PreferencesDocument{"density": "compact"}})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if result.Prefs["density"] != "compact" {
		t.Errorf("prefs.density=%v, want compact", result.Prefs["density"])
	}
}

func TestPreferences_Set_RejectsMissingPrefs(t *testing.T) {
	srv, _ := newServerCapturing(t, http.StatusOK, `{}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Preferences.Set(context.Background(), &PreferencesSetInput{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Errorf("expected *ValidationError, got %T", err)
	}
}

// ---------------------- Cases -------------------------------------

func TestCases_Create_HappyPath(t *testing.T) {
	resp := `{"object":"case","hash":"abc123","url":"https://share.example/case/abc123","readonly":true,"created_at":"2026-05-20T14:32:01Z"}`
	srv, cap := newServerCapturing(t, http.StatusOK, resp)
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Cases.Create(context.Background(), &CaseCreateInput{
		Input:    map[string]any{"applicant": map[string]any{"name": "John Doe"}},
		Results:  map[string]any{"decided": true},
		Products: []string{"senior-life"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Hash != "abc123" {
		t.Errorf("hash=%q, want abc123", result.Hash)
	}
	if !result.Readonly {
		t.Errorf("readonly=false, want true")
	}
	if cap.method != http.MethodPost || cap.path != caseCreatePath {
		t.Errorf("captured %s %s, want POST %s", cap.method, cap.path, caseCreatePath)
	}
	if cap.idem == "" {
		t.Errorf("expected Idempotency-Key header on POST")
	}
	var wire caseCreateWireBody
	if err := json.Unmarshal(cap.body, &wire); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if len(wire.Products) != 1 || wire.Products[0] != "senior-life" {
		t.Errorf("products=%v, want [senior-life]", wire.Products)
	}
}

func TestCases_Create_AcceptsRawXMLInput(t *testing.T) {
	srv, cap := newServerCapturing(t, http.StatusOK, `{"object":"case","hash":"x","url":"","readonly":false,"created_at":""}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Cases.Create(context.Background(), &CaseCreateInput{Input: "<applicant/>"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var wire caseCreateWireBody
	if err := json.Unmarshal(cap.body, &wire); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if s, ok := wire.Input.(string); !ok || s != "<applicant/>" {
		t.Errorf("input=%v, want raw XML string", wire.Input)
	}
}

func TestCases_Create_RejectsMissingInput(t *testing.T) {
	srv, _ := newServerCapturing(t, http.StatusOK, `{}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Cases.Create(context.Background(), &CaseCreateInput{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestCases_Create_500Errors(t *testing.T) {
	body := `{"type":"about:blank","title":"server","status":500,"code":"server_error"}`
	srv, _ := newServerCapturing(t, http.StatusInternalServerError, body)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Cases.Create(context.Background(), &CaseCreateInput{Input: map[string]any{"a": 1}})
	if err == nil {
		t.Fatalf("expected error on 500")
	}
}

// ---------------------- Email -------------------------------------

func TestEmail_Enqueue_SerializesBase64Attachment(t *testing.T) {
	srv, cap := newServerCapturing(t, http.StatusOK, `{"enqueue_id":"eq_1"}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Email.Enqueue(context.Background(), &EmailEnqueueInput{
		To:                 "jane@smith.com",
		Subject:            "Your case",
		BodyHTML:           "<p>Hi</p>",
		AttachmentFilename: "case-1.pdf",
		AttachmentContent:  []byte("PDF-bytes"),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if result.EnqueueID != "eq_1" {
		t.Errorf("enqueue_id=%q, want eq_1", result.EnqueueID)
	}
	if cap.method != http.MethodPost || cap.path != emailEnqueuePath {
		t.Errorf("captured %s %s, want POST %s", cap.method, cap.path, emailEnqueuePath)
	}
	if !strings.Contains(string(cap.body), "content_base64") {
		t.Errorf("body missing content_base64 field: %s", string(cap.body))
	}
}

func TestEmail_Enqueue_RejectsMissingTo(t *testing.T) {
	srv, _ := newServerCapturing(t, http.StatusOK, `{}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Email.Enqueue(context.Background(), &EmailEnqueueInput{To: "", Subject: "s", BodyHTML: "b"})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestCases_Email_TargetsEnqueueEndpoint(t *testing.T) {
	srv, cap := newServerCapturing(t, http.StatusOK, `{"enqueue_id":"eq_2"}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Cases.Email(context.Background(), &EmailEnqueueInput{To: "jane@smith.com", Subject: "s", BodyHTML: "b"})
	if err != nil {
		t.Fatalf("Email: %v", err)
	}
	if cap.path != emailEnqueuePath {
		t.Errorf("captured path=%q, want %s", cap.path, emailEnqueuePath)
	}
}
