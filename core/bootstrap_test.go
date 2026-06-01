// Bytewise conformance gate for the embedded HMAC bootstrap signature.
//
// The fixture at tests/conformance/fixtures/auth-vector.json (repo root)
// is the binding contract. This Go SDK MUST reproduce the identical hex
// against the same inputs as the TypeScript, Python, PHP, and C# SDKs.
//
// If this test fails after an intentional change to the auth wire format,
// regenerate the fixture, update api/guides/authentication-advanced.md,
// and bump every SDK's major version — the change is breaking.

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type authVectorFixture struct {
	Inputs struct {
		Keycode    string `json:"keycode"`
		Email      string `json:"email"`
		LicenseKey string `json:"licenseKey"`
		DeviceID   string `json:"deviceId"`
		Method     string `json:"method"`
		Path       string `json:"path"`
		Timestamp  int64  `json:"timestamp"`
	} `json:"inputs"`
	SerializedBody string `json:"serializedBody"`
	Canonical      string `json:"canonical"`
	Expected       struct {
		Algorithm string `json:"algorithm"`
		Hex       string `json:"hex"`
		Header    string `json:"header"`
	} `json:"expected"`
}

func loadAuthVector(t *testing.T) authVectorFixture {
	t.Helper()
	// packages/go/core → repo root is four levels up.
	path := filepath.Join("..", "..", "..", "tests", "conformance", "fixtures", "auth-vector.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read auth-vector fixture at %s: %v", path, err)
	}
	var fixture authVectorFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode auth-vector fixture: %v", err)
	}
	return fixture
}

func TestBootstrapSignature_ConformanceVector(t *testing.T) {
	fx := loadAuthVector(t)
	sig, err := BuildBootstrapSignature(BootstrapInput{
		Keycode:    fx.Inputs.Keycode,
		Email:      fx.Inputs.Email,
		LicenseKey: fx.Inputs.LicenseKey,
		DeviceID:   fx.Inputs.DeviceID,
		Method:     fx.Inputs.Method,
		Path:       fx.Inputs.Path,
		Timestamp:  fx.Inputs.Timestamp,
	})
	if err != nil {
		t.Fatalf("BuildBootstrapSignature: %v", err)
	}
	if sig.SerializedBody != fx.SerializedBody {
		t.Errorf("serializedBody mismatch\n  got:  %q\n  want: %q", sig.SerializedBody, fx.SerializedBody)
	}
	if sig.Canonical != fx.Canonical {
		t.Errorf("canonical mismatch\n  got:  %q\n  want: %q", sig.Canonical, fx.Canonical)
	}
	if sig.Hex != fx.Expected.Hex {
		t.Errorf("hex mismatch\n  got:  %s\n  want: %s", sig.Hex, fx.Expected.Hex)
	}
	if "ISA-Signature: "+sig.Header != fx.Expected.Header {
		t.Errorf("header mismatch\n  got:  %s\n  want: %s", "ISA-Signature: "+sig.Header, fx.Expected.Header)
	}
}

func TestBootstrapSignature_DeviceIDOnlyInBody(t *testing.T) {
	// Anti-regression: an earlier draft included deviceId in the canonical
	// path. Locked spec sends it as X-Device-ID header only; the only
	// canonical appearance is inside the body JSON for POST /v1/sessions.
	fx := loadAuthVector(t)
	bodyStart := strings.Index(fx.Canonical, fx.SerializedBody)
	if bodyStart < 0 {
		t.Fatalf("fixture canonical does not contain serializedBody")
	}
	before := fx.Canonical[:bodyStart]
	if strings.Contains(before, fx.Inputs.DeviceID) {
		t.Errorf("deviceId leaked into canonical prefix: %q", before)
	}
}

func TestBootstrapSignature_SerializesUnicodeBody(t *testing.T) {
	body := serializeBootstrapBody("SDV-HWH-WDD", "jose.garcia@example.com", "device-é-東京")
	want := `{"keycode":"SDV-HWH-WDD","email":"jose.garcia@example.com","deviceId":"device-é-東京"}`
	if body != want {
		t.Fatalf("serialized unicode body mismatch\n  got:  %q\n  want: %q", body, want)
	}
}

func TestBootstrapSignature_EscapesLineSeparators(t *testing.T) {
	body := serializeBootstrapBody("SDV-HWH-WDD", "jose.garcia@example.com", "device-\u2028-\u2029")
	want := `{"keycode":"SDV-HWH-WDD","email":"jose.garcia@example.com","deviceId":"device-\u2028-\u2029"}`
	if body != want {
		t.Fatalf("serialized line separator body mismatch\n  got:  %q\n  want: %q", body, want)
	}
}

func TestBootstrapSignature_RequiredFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*BootstrapInput)
	}{
		{"missing keycode", func(b *BootstrapInput) { b.Keycode = "" }},
		{"missing email", func(b *BootstrapInput) { b.Email = "" }},
		{"missing licenseKey", func(b *BootstrapInput) { b.LicenseKey = "" }},
		{"missing deviceId", func(b *BootstrapInput) { b.DeviceID = "" }},
		{"missing method", func(b *BootstrapInput) { b.Method = "" }},
		{"missing path", func(b *BootstrapInput) { b.Path = "" }},
		{"missing timestamp", func(b *BootstrapInput) { b.Timestamp = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := BootstrapInput{
				Keycode: "k", Email: "e", LicenseKey: "l", DeviceID: "d",
				Method: "POST", Path: "/v1/sessions", Timestamp: 1,
			}
			tc.mut(&in)
			if _, err := BuildBootstrapSignature(in); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
