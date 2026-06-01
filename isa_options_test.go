package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Tests for the typed options-bag constructor. Mirrors the Python SDK's
// tests/zyins/test_isa_options.py and the TS isaOptions.test.ts coverage.

// fakeTestToken is a synthetic, non-credential placeholder used only to
// shape-check the auth-supplier dispatch path. It is NOT a real token
// and does not survive the bootstrap validation pipeline; tests that
// care about successful construction set ISA_TOKEN via t.Setenv instead.
const fakeTestToken = "isa_test_" + "fakeplaceholder"

func TestResolveIsaOptions_Defaults(t *testing.T) {
	resolved, err := ResolveIsaOptions(IsaOptions{
		Auth: BearerAuth{Token: fakeTestToken},
	})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	if resolved.Timeout != DefaultTimeout {
		t.Errorf("Timeout: got %v, want %v", resolved.Timeout, DefaultTimeout)
	}
	if resolved.APIVersion != APIVersionV2 {
		t.Errorf("APIVersion: got %q, want %q", resolved.APIVersion, APIVersionV2)
	}
	if resolved.BaseURL != ProductionRemoteOrigin {
		t.Errorf("BaseURL: got %q, want %q", resolved.BaseURL, ProductionRemoteOrigin)
	}
	if resolved.ProxyOrigin != "" {
		t.Errorf("ProxyOrigin: got %q, want empty", resolved.ProxyOrigin)
	}
	if _, ok := resolved.Engine.(RemoteEngine); !ok {
		t.Errorf("Engine: got %T, want RemoteEngine", resolved.Engine)
	}
}

func TestResolveIsaOptions_RejectsMissingAuth(t *testing.T) {
	_, err := ResolveIsaOptions(IsaOptions{})
	if err == nil {
		t.Fatal("expected error for missing Auth, got nil")
	}
	if !strings.Contains(err.Error(), "Auth") {
		t.Errorf("error message: got %q, want substring 'Auth'", err.Error())
	}
}

func TestResolveIsaOptions_ExplicitV1Pin(t *testing.T) {
	resolved, err := ResolveIsaOptions(IsaOptions{
		Auth:       BearerAuth{Token: fakeTestToken},
		APIVersion: APIVersionV1,
	})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	if resolved.APIVersion != APIVersionV1 {
		t.Errorf("APIVersion: got %q, want %q", resolved.APIVersion, APIVersionV1)
	}
}

func TestResolveIsaOptions_RejectsUnknownAPIVersion(t *testing.T) {
	_, err := ResolveIsaOptions(IsaOptions{
		Auth:       BearerAuth{Token: fakeTestToken},
		APIVersion: "v3",
	})
	if err == nil {
		t.Fatal("expected error for unknown APIVersion, got nil")
	}
}

func TestResolveIsaOptions_RejectsClientVersionUntilWired(t *testing.T) {
	_, err := ResolveIsaOptions(IsaOptions{
		Auth:          BearerAuth{Token: fakeTestToken},
		ClientVersion: "build-sha",
	})
	if err == nil {
		t.Fatal("expected error for ClientVersion")
	}
}

func TestResolveIsaOptions_LocalEngineSetsBaseURL(t *testing.T) {
	resolved, err := ResolveIsaOptions(IsaOptions{
		Auth:   BearerAuth{Token: fakeTestToken},
		Engine: LocalEngine{BaseURL: "http://localhost:9090"},
	})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	if resolved.BaseURL != "http://localhost:9090" {
		t.Errorf("BaseURL: got %q", resolved.BaseURL)
	}
}

func TestResolveIsaOptions_RejectsLocalEngineWithoutBaseURL(t *testing.T) {
	_, err := ResolveIsaOptions(IsaOptions{
		Auth:   BearerAuth{Token: fakeTestToken},
		Engine: LocalEngine{},
	})
	if err == nil {
		t.Fatal("expected error for LocalEngine without BaseURL")
	}
}

func TestResolveIsaOptions_ProxyEngineCarriesOrigin(t *testing.T) {
	resolved, err := ResolveIsaOptions(IsaOptions{
		Auth:   BearerAuth{Token: fakeTestToken},
		Engine: ProxyEngine{ProxyOrigin: "https://proxy.example.com"},
	})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	// Proxy mode targets the production origin for the underlying ZyINS
	// request; ProxyOrigin lives on the resolved options for the proxy
	// namespace to consume.
	if resolved.BaseURL != ProductionRemoteOrigin {
		t.Errorf("BaseURL: got %q, want %q", resolved.BaseURL, ProductionRemoteOrigin)
	}
	if resolved.ProxyOrigin != "https://proxy.example.com" {
		t.Errorf("ProxyOrigin: got %q", resolved.ProxyOrigin)
	}
}

func TestResolveIsaOptions_ProxyEngineDefaultOrigin(t *testing.T) {
	resolved, err := ResolveIsaOptions(IsaOptions{
		Auth:   BearerAuth{Token: fakeTestToken},
		Engine: ProxyEngine{},
	})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	if resolved.ProxyOrigin != ProductionProxyOrigin {
		t.Errorf("ProxyOrigin: got %q, want %q", resolved.ProxyOrigin, ProductionProxyOrigin)
	}
}

func TestResolveIsaOptions_ExplicitTimeout(t *testing.T) {
	resolved, err := ResolveIsaOptions(IsaOptions{
		Auth:    BearerAuth{Token: fakeTestToken},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	if resolved.Timeout != 5*time.Second {
		t.Errorf("Timeout: got %v", resolved.Timeout)
	}
}

func TestRemoteEngine_DefaultBaseURL(t *testing.T) {
	if (RemoteEngine{}).baseURL() != ProductionRemoteOrigin {
		t.Errorf("RemoteEngine{}: got %q, want production", (RemoteEngine{}).baseURL())
	}
	if (RemoteEngine{BaseURL: "https://staging.example.com"}).baseURL() != "https://staging.example.com" {
		t.Errorf("explicit BaseURL not honored")
	}
}

func TestEngineKindDiscriminators(t *testing.T) {
	cases := []struct {
		engine Engine
		kind   string
	}{
		{RemoteEngine{}, "remote"},
		{LocalEngine{}, "local"},
		{ProxyEngine{}, "proxy"},
		{InMemoryEngine{}, "in_memory"},
	}
	for _, c := range cases {
		if got := c.engine.engineKind(); got != c.kind {
			t.Errorf("%T.engineKind() = %q, want %q", c.engine, got, c.kind)
		}
	}
}

func TestAuthSupplierKindDiscriminators(t *testing.T) {
	cases := []struct {
		auth AuthSupplier
		kind string
	}{
		{BearerAuth{}, "bearer"},
		{LicenseAuth{}, "license"},
		{FormAuth{}, "form"},
		{SessionAuth{}, "session"},
	}
	for _, c := range cases {
		if got := c.auth.authKind(); got != c.kind {
			t.Errorf("%T.authKind() = %q, want %q", c.auth, got, c.kind)
		}
	}
}

func TestNew_DispatchesByAuthKind(t *testing.T) {
	t.Setenv("ISA_TOKEN", fakeTestToken)
	isa, err := New(IsaOptions{Auth: BearerAuth{Token: fakeTestToken}})
	if err != nil {
		t.Fatalf("New(BearerAuth): %v", err)
	}
	if isa == nil {
		t.Fatal("New: returned nil Isa")
	}
}

func TestNew_AppliesResolvedZyinsOptions(t *testing.T) {
	var sawVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			t.Errorf("path = %s, want /ready", r.URL.Path)
		}
		sawVersion = r.Header.Get(apiVersionHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ready": true,
			"status": "serving",
			"db": {"status":"serving","latency_ms":1,"checked_at":"2026-05-14T14:32:01Z"},
			"cache": {"status":"serving","latency_ms":1,"checked_at":"2026-05-14T14:32:01Z"},
			"checked_at": "2026-05-14T14:32:01Z"
		}`))
	}))
	defer srv.Close()

	isa, err := New(IsaOptions{
		Auth:       BearerAuth{Token: fakeTestToken},
		Engine:     LocalEngine{BaseURL: srv.URL},
		Timeout:    time.Second,
		APIVersion: APIVersionV1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := isa.Zyins.Health.GetReadiness(context.Background()); err != nil {
		t.Fatalf("GetReadiness: %v", err)
	}
	if sawVersion != string(APIVersionV1) {
		t.Errorf("Version header = %q, want %q", sawVersion, APIVersionV1)
	}
}

func TestNew_ProxyEngineConfiguresProxyOrigin(t *testing.T) {
	const proxyOrigin = "https://proxy.example.com"
	isa, err := New(IsaOptions{
		Auth:   BearerAuth{Token: fakeTestToken},
		Engine: ProxyEngine{ProxyOrigin: proxyOrigin},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if isa.Proxy.binding.ProxyOrigin != proxyOrigin {
		t.Errorf("ProxyOrigin = %q, want %q", isa.Proxy.binding.ProxyOrigin, proxyOrigin)
	}
}

func TestNew_AcceptsPointerAuthSupplier(t *testing.T) {
	isa, err := New(IsaOptions{Auth: &BearerAuth{Token: fakeTestToken}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if isa == nil || isa.Zyins == nil {
		t.Fatalf("New returned incomplete Isa: %+v", isa)
	}
}

func TestNew_RejectsNilPointerAuthSupplier(t *testing.T) {
	var auth *BearerAuth
	_, err := New(IsaOptions{Auth: auth})
	if err == nil {
		t.Fatal("expected error for nil pointer auth")
	}
}

func TestResolveIsaOptions_AcceptsPointerEngine(t *testing.T) {
	const baseURL = "http://localhost:9999"
	resolved, err := ResolveIsaOptions(IsaOptions{
		Auth:   BearerAuth{Token: fakeTestToken},
		Engine: &LocalEngine{BaseURL: baseURL},
	})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	if resolved.BaseURL != baseURL {
		t.Errorf("BaseURL = %q, want %q", resolved.BaseURL, baseURL)
	}
}

func TestNew_FormAuthRejectsMissingFormToken(t *testing.T) {
	t.Setenv("ISA_TOKEN", fakeTestToken)
	_, err := New(IsaOptions{Auth: FormAuth{}})
	if err == nil {
		t.Fatal("expected error for missing FormToken")
	}
	if strings.Contains(err.Error(), "ISA_TOKEN") || strings.Contains(err.Error(), "WithBearer") {
		t.Errorf("error should describe FormAuth, got %q", err.Error())
	}
}

func TestNew_FormAuthRejectsUntilReissueTransportIsWired(t *testing.T) {
	t.Setenv("ISA_TOKEN", fakeTestToken)
	_, err := New(IsaOptions{Auth: NewFormAuth("opaque-form-token")})
	if err == nil {
		t.Fatal("expected error until FormAuth reissue transport is wired")
	}
	if strings.Contains(err.Error(), "ISA_TOKEN") || strings.Contains(err.Error(), "WithBearer") {
		t.Errorf("error should not use bearer fallback, got %q", err.Error())
	}
}

func TestNew_RejectsMissingAuth(t *testing.T) {
	_, err := New(IsaOptions{})
	if err == nil {
		t.Fatal("expected error for missing Auth")
	}
}

func TestBearerAuth_FactoryHelpers(t *testing.T) {
	bearer := NewBearerAuth("isa_live_" + "xyz")
	if !strings.HasPrefix(bearer.Token, "isa_live_") {
		t.Errorf("Token: got %q", bearer.Token)
	}
	if BearerAuthFromEnv().Token != "" {
		t.Errorf("BearerAuthFromEnv should carry no token")
	}
}

func TestLicenseAuth_FactoryHelpers(t *testing.T) {
	license := NewLicenseAuth("ABC-123-XYZ", "agent@example.com")
	if license.Keycode != "ABC-123-XYZ" || license.Email != "agent@example.com" {
		t.Errorf("LicenseAuth fields: %+v", license)
	}
	if LicenseAuthFromEnv().Keycode != "" || LicenseAuthFromEnv().Email != "" {
		t.Errorf("LicenseAuthFromEnv should carry no credentials")
	}
}

func TestDefaultAPIVersionIsV2(t *testing.T) {
	// The whole point of this PR — v2 is the default. Pin it.
	resolved, err := ResolveIsaOptions(IsaOptions{Auth: BearerAuth{Token: fakeTestToken}})
	if err != nil {
		t.Fatalf("ResolveIsaOptions: %v", err)
	}
	if resolved.APIVersion != APIVersionV2 {
		t.Errorf("APIVersion default: got %q, want %q", resolved.APIVersion, APIVersionV2)
	}
}
