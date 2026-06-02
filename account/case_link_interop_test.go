package account

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// Cross-SDK case-link + case-crypto interop — Go SDK in-process gate.
//
// Reads the shared fixture conformance/scenarios/.fixtures/case-link-share.fixture.json
// (produced from the canonical TypeScript WebCrypto stack) and proves:
//
//   - Go assembles the byte-identical single-segment link from (base, code, key).
//   - Go parses both the single-segment and legacy /c/ link forms to the same
//     (code, keyFragment) every other SDK produces.
//   - A case encrypted by TypeScript decrypts in Go for both the 128-bit and
//     256-bit envelopes (the "encrypted-in-X decrypts-in-all" matrix row).
//   - Go round-trips its own encrypt → decrypt back to the original payload.
//
// Sibling tests in packages/python, packages/php, and packages/csharp read the
// same fixture, so any divergence fails the diverging language's own CI job.

type caseLinkFixture struct {
	Product            string         `json:"product"`
	PlaintextJSON      string         `json:"plaintext_json"`
	Payload            map[string]any `json:"payload"`
	ViewerBaseURL      string         `json:"viewer_base_url"`
	Code               string         `json:"code"`
	Envelope128        CaseEnvelope   `json:"envelope_128"`
	KeyFragment128     string         `json:"key_fragment_128"`
	Envelope256        CaseEnvelope   `json:"envelope_256"`
	KeyFragment256     string         `json:"key_fragment_256"`
	ExpectedLinkSingle string         `json:"expected_link_single_segment"`
	ExpectedLinkLegacy string         `json:"expected_link_legacy_c"`
	ParseCases         []struct {
		Link                string `json:"link"`
		ExpectedCode        string `json:"expected_code"`
		ExpectedKeyFragment string `json:"expected_key_fragment"`
	} `json:"parse_cases"`
}

func loadCaseLinkFixture(t *testing.T) caseLinkFixture {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "conformance", "scenarios", ".fixtures", "case-link-share.fixture.json")
		if data, err := os.ReadFile(candidate); err == nil {
			var fx caseLinkFixture
			if err := json.Unmarshal(data, &fx); err != nil {
				t.Fatalf("decode case-link fixture: %v", err)
			}
			return fx
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("case-link-share.fixture.json not found walking up from test file")
	return caseLinkFixture{}
}

func TestAssembleLink_SingleSegment_MatchesSharedFixture(t *testing.T) {
	fx := loadCaseLinkFixture(t)
	got := AssembleLink(fx.ViewerBaseURL, fx.Code, fx.KeyFragment128)
	if got != fx.ExpectedLinkSingle {
		t.Errorf("AssembleLink = %q, want %q", got, fx.ExpectedLinkSingle)
	}
}

func TestParseLink_BothForms_MatchSharedFixture(t *testing.T) {
	fx := loadCaseLinkFixture(t)
	for _, tc := range fx.ParseCases {
		parsed, err := ParseLink(tc.Link)
		if err != nil {
			t.Fatalf("ParseLink(%q): %v", tc.Link, err)
		}
		if parsed.Code != tc.ExpectedCode {
			t.Errorf("ParseLink(%q).Code = %q, want %q", tc.Link, parsed.Code, tc.ExpectedCode)
		}
		if parsed.KeyFragment != tc.ExpectedKeyFragment {
			t.Errorf("ParseLink(%q).KeyFragment = %q, want %q", tc.Link, parsed.KeyFragment, tc.ExpectedKeyFragment)
		}
	}
}

func TestParseLink_RejectsMissingFragment(t *testing.T) {
	if _, err := ParseLink("https://link.isaapi.com/abc123"); err == nil {
		t.Error("ParseLink without #k= should error")
	}
	if _, err := ParseLink("https://link.isaapi.com/abc123#k="); err == nil {
		t.Error("ParseLink with empty fragment should error")
	}
	if _, err := ParseLink(""); err == nil {
		t.Error("ParseLink empty link should error")
	}
}

func TestDecryptCase_TypeScriptEnvelope_128(t *testing.T) {
	fx := loadCaseLinkFixture(t)
	got, err := DecryptCase(fx.Product, fx.Envelope128, fx.KeyFragment128)
	if err != nil {
		t.Fatalf("DecryptCase 128-bit TS envelope: %v", err)
	}
	assertPayloadMatchesFixture(t, got, fx)
}

func TestDecryptCase_TypeScriptEnvelope_256(t *testing.T) {
	fx := loadCaseLinkFixture(t)
	got, err := DecryptCase(fx.Product, fx.Envelope256, fx.KeyFragment256)
	if err != nil {
		t.Fatalf("DecryptCase 256-bit TS envelope: %v", err)
	}
	assertPayloadMatchesFixture(t, got, fx)
}

func TestDecryptCase_WrongProduct_FailsAuthentication(t *testing.T) {
	fx := loadCaseLinkFixture(t)
	if _, err := DecryptCase("eapp", fx.Envelope128, fx.KeyFragment128); err == nil {
		t.Error("DecryptCase with wrong product AAD should fail authentication")
	}
}

func TestEncryptCase_RoundTrip(t *testing.T) {
	fx := loadCaseLinkFixture(t)
	encrypted, err := EncryptCase(fx.Product, fx.Payload, SystemRandomBytes)
	if err != nil {
		t.Fatalf("EncryptCase: %v", err)
	}
	got, err := DecryptCase(fx.Product, encrypted.Envelope, encrypted.KeyFragment)
	if err != nil {
		t.Fatalf("DecryptCase round-trip: %v", err)
	}
	if !reflect.DeepEqual(normalizeJSON(t, got), normalizeJSON(t, fx.Payload)) {
		t.Errorf("round-trip payload mismatch: got %#v", got)
	}
}

// assertPayloadMatchesFixture compares a decrypted payload to the fixture's
// canonical payload through a JSON round-trip so numeric types align.
func assertPayloadMatchesFixture(t *testing.T, got any, fx caseLinkFixture) {
	t.Helper()
	if !reflect.DeepEqual(normalizeJSON(t, got), normalizeJSON(t, fx.Payload)) {
		t.Errorf("decrypted payload = %#v, want %#v", got, fx.Payload)
	}
}

func normalizeJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("normalize marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("normalize unmarshal: %v", err)
	}
	return out
}
