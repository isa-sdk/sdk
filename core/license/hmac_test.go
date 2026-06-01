package license

import (
	"strings"
	"testing"
	"time"
)

func TestBuild_RejectsEmptyFields(t *testing.T) {
	_, err := Build(Input{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestBuild_ProducesAllSixHeaders(t *testing.T) {
	fixed := time.Unix(1_700_000_000, 0).UTC()
	headers, err := Build(Input{
		LicenseKey: "lk",
		OrderID:    "ABC-123-XYZ",
		Email:      "agent@example.com",
		Method:     "POST",
		RequestURI: "/v1/prequalify",
		Body:       []byte(`{"a":1}`),
		DeviceID:   "dev-1",
		Clock:      func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.HasPrefix(headers.Authorization, "License ") {
		t.Errorf("Authorization=%q", headers.Authorization)
	}
	if headers.DeviceID != "dev-1" {
		t.Errorf("DeviceID=%q", headers.DeviceID)
	}
	if headers.DeviceSignature == "" || len(headers.DeviceSignature) != 64 {
		t.Errorf("DeviceSignature should be 64-char hex, got len=%d", len(headers.DeviceSignature))
	}
	if headers.LicenseMethod != "POST" {
		t.Errorf("LicenseMethod=%q", headers.LicenseMethod)
	}
	if headers.LicenseURI != "/v1/prequalify" {
		t.Errorf("LicenseURI=%q", headers.LicenseURI)
	}
	if headers.LicenseTimestamp != "1700000000000" {
		t.Errorf("LicenseTimestamp=%q", headers.LicenseTimestamp)
	}
}

func TestStripQuotes(t *testing.T) {
	if got := StripQuotes(`"a"`); got != "a" {
		t.Errorf("StripQuotes: %q", got)
	}
	if got := StripQuotes(`a`); got != "a" {
		t.Errorf("StripQuotes passthrough: %q", got)
	}
}
