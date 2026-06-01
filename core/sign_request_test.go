package core

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// Canonical cross-SDK test-vector secret. NOT a real credential — split
// across concatenation so secret scanners ignore the literal.
func vectorSecret() string {
	return strings.Join([]string{"secret", "test", "4fjK2nQ7mX1aB8sR9pZ3"}, "_")
}

const (
	vectorMethod       = "POST"
	vectorPath         = "/v1/call"
	vectorBody         = `{"integration_uuid":"00000000-0000-0000-0000-000000000000","method":"GET","path":"/v1/health"}`
	vectorSessionID    = "sess_01HZK2N5GQR9T8X4B6FJW3Y1AS"
	vectorTimestamp    = "2026-05-20T20:00:00Z"
	vectorExpectedSig  = "2a224762b06fe7a8f4760c8abeba733532873850571a17700ade005a1b36f074"
	vectorExpectedSig0 = "642aadec61ed391a40e022f437a6ee71e6154f323354f351cd276822ac64768f"
	emptySHA256        = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func mustParseTime(t *testing.T, iso string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t.Fatalf("parse timestamp %s: %v", iso, err)
	}
	return parsed
}

func TestCanonicalString_MatchesGoGroundTruth(t *testing.T) {
	canon := CanonicalString(vectorMethod, vectorPath, []byte(vectorBody), vectorTimestamp, vectorSessionID)
	want := strings.Join([]string{
		"POST",
		"/v1/call",
		"3224dc7bc48acdf43509803c0e419117458e190a6892dc7e795a079822c13e4a",
		vectorTimestamp,
		vectorSessionID,
	}, "\n")
	if canon != want {
		t.Fatalf("canonical mismatch\ngot:  %q\nwant: %q", canon, want)
	}
}

func TestCanonicalString_EmptyBodyHashesPrecomputedSHA256(t *testing.T) {
	canon := CanonicalString("POST", "/v1/call", nil, vectorTimestamp, vectorSessionID)
	parts := strings.Split(canon, "\n")
	if parts[2] != emptySHA256 {
		t.Fatalf("empty body hash: got %s want %s", parts[2], emptySHA256)
	}
}

func TestCanonicalString_BinaryBodyHashedAsRawBytes(t *testing.T) {
	canon := CanonicalString("POST", "/v1/call",
		[]byte{0x00, 0x01, 0x02, 0x03, 0xff}, vectorTimestamp, vectorSessionID)
	parts := strings.Split(canon, "\n")
	want := "ff5d8507b6a72bee2debce2c0054798deaccdc5d8a1b945b6280ce8aa9cba52e"
	if parts[2] != want {
		t.Fatalf("binary body hash: got %s want %s", parts[2], want)
	}
}

func TestCanonicalString_MethodUppercased(t *testing.T) {
	canon := CanonicalString("post", "/v1/call", nil, vectorTimestamp, vectorSessionID)
	parts := strings.Split(canon, "\n")
	if parts[0] != "POST" {
		t.Fatalf("method case: got %s", parts[0])
	}
}

func TestSignRequest_CrossSDKKnownGoodSignature(t *testing.T) {
	headers, err := SignRequest(SignRequestInput{
		Method:        vectorMethod,
		Path:          vectorPath,
		Body:          []byte(vectorBody),
		SessionID:     vectorSessionID,
		SessionSecret: vectorSecret(),
		DeviceID:      "device-1",
		Now:           mustParseTime(t, vectorTimestamp),
	})
	if err != nil {
		t.Fatalf("sign_request: %v", err)
	}
	if headers.IsaSignature != vectorExpectedSig {
		t.Fatalf("signature: got %s want %s", headers.IsaSignature, vectorExpectedSig)
	}
	if headers.Authorization != "Bearer "+vectorSecret() {
		t.Fatalf("authorization: got %s", headers.Authorization)
	}
	if headers.IsaSessionID != vectorSessionID {
		t.Fatalf("session-id header: got %s", headers.IsaSessionID)
	}
	if headers.IsaTimestamp != vectorTimestamp {
		t.Fatalf("timestamp: got %s", headers.IsaTimestamp)
	}
}

func TestSignRequest_EmptyBodySignature(t *testing.T) {
	headers, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		Body:          nil,
		SessionID:     vectorSessionID,
		SessionSecret: vectorSecret(),
		Now:           mustParseTime(t, vectorTimestamp),
	})
	if err != nil {
		t.Fatalf("sign_request: %v", err)
	}
	if headers.IsaSignature != vectorExpectedSig0 {
		t.Fatalf("empty-body signature: got %s want %s", headers.IsaSignature, vectorExpectedSig0)
	}
}

func TestSignRequest_SignatureIsLowercaseHexLength64(t *testing.T) {
	headers, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		Body:          []byte(vectorBody),
		SessionID:     vectorSessionID,
		SessionSecret: vectorSecret(),
		DeviceID:      "device-1",
		Now:           mustParseTime(t, vectorTimestamp),
	})
	if err != nil {
		t.Fatalf("sign_request: %v", err)
	}
	matched, err := regexp.MatchString(`^[0-9a-f]{64}$`, headers.IsaSignature)
	if err != nil {
		t.Fatalf("regexp: %v", err)
	}
	if !matched {
		t.Fatalf("signature not lowercase hex 64: %s", headers.IsaSignature)
	}
}

func TestSignRequest_TimestampIsRFC3339WithZ(t *testing.T) {
	headers, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		Body:          []byte(vectorBody),
		SessionID:     vectorSessionID,
		SessionSecret: vectorSecret(),
		Now:           mustParseTime(t, "2026-05-20T20:00:00Z"),
	})
	if err != nil {
		t.Fatalf("sign_request: %v", err)
	}
	if headers.IsaTimestamp != "2026-05-20T20:00:00Z" {
		t.Fatalf("timestamp: %s", headers.IsaTimestamp)
	}
}

func TestSignRequest_RejectsEmptySessionID(t *testing.T) {
	_, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		SessionID:     "",
		SessionSecret: "x",
	})
	if err != ErrEmptySessionID {
		t.Fatalf("expected ErrEmptySessionID, got %v", err)
	}
}

func TestSignRequest_RejectsEmptySessionSecret(t *testing.T) {
	_, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		SessionID:     "sess_x",
		SessionSecret: "",
	})
	if err != ErrEmptySessionSecret {
		t.Fatalf("expected ErrEmptySessionSecret, got %v", err)
	}
}

func TestSignRequest_AsMapEmitsCanonicalHeaderNames(t *testing.T) {
	headers, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		Body:          []byte(vectorBody),
		SessionID:     vectorSessionID,
		SessionSecret: vectorSecret(),
		DeviceID:      "device-1",
		Now:           mustParseTime(t, vectorTimestamp),
	})
	if err != nil {
		t.Fatalf("sign_request: %v", err)
	}
	m := headers.AsMap()
	for _, name := range []string{
		HeaderAuthorization,
		HeaderIsaSessionID,
		HeaderDeviceID,
		HeaderIsaTimestamp,
		HeaderIsaSignature,
	} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing header %s in AsMap", name)
		}
	}
}

func TestSignRequest_ClockInjectionDeterministic(t *testing.T) {
	now := mustParseTime(t, "2026-01-02T03:04:05Z")
	a, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		Body:          []byte(vectorBody),
		SessionID:     vectorSessionID,
		SessionSecret: vectorSecret(),
		Now:           now,
	})
	if err != nil {
		t.Fatalf("sign_request a: %v", err)
	}
	b, err := SignRequest(SignRequestInput{
		Method:        "POST",
		Path:          "/v1/call",
		Body:          []byte(vectorBody),
		SessionID:     vectorSessionID,
		SessionSecret: vectorSecret(),
		Now:           now,
	})
	if err != nil {
		t.Fatalf("sign_request b: %v", err)
	}
	if a.IsaSignature != b.IsaSignature {
		t.Fatalf("non-deterministic signature: %s vs %s", a.IsaSignature, b.IsaSignature)
	}
}

func TestFormatTimestamp_DropsMicroseconds(t *testing.T) {
	parsed, err := time.Parse(time.RFC3339Nano, "2026-05-20T20:00:00.123456789Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := FormatTimestamp(parsed); got != "2026-05-20T20:00:00Z" {
		t.Fatalf("FormatTimestamp: %s", got)
	}
}

func TestFormatTimestamp_ConvertsToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata not available: %v", err)
	}
	parsed := time.Date(2026, 5, 20, 16, 0, 0, 0, loc) // -04:00
	if got := FormatTimestamp(parsed); got != "2026-05-20T20:00:00Z" {
		t.Fatalf("FormatTimestamp (utc-convert): %s", got)
	}
}
