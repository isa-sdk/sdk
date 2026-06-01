// Provides: the embedded HMAC bootstrap signature for POST /v1/sessions.
//
// This module pins the byte-exact wire format documented at
// api/guides/authentication-advanced.md#test-vector and reproduced in
// tests/conformance/fixtures/auth-vector.json. The reference TypeScript
// implementation lives at packages/ts/src/core/internal/auth/bootstrap.ts;
// this file MUST reproduce the identical hex against the same inputs.
//
// Two-stage flow:
//  1. Serialize the request body as JSON, keys in source order
//     (keycode, email, deviceId), no whitespace, no trailing newline.
//  2. Build the canonical signing string and HMAC-SHA256 it with the
//     licenseKey as the key.
//
// Why a dedicated function: the bootstrap signature predates any session
// (no sessionSecret exists yet), uses the licenseKey as the HMAC key, and
// is the only call where deviceId appears in the request body. The
// steady-state session-signing helper (SignRequest) handles all other
// calls.

package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// BootstrapInput mirrors the auth-vector fixture one-for-one. Field
// ORDER matters for SerializedBody.
type BootstrapInput struct {
	// Keycode is the per-seat keycode (e.g. SDV-HWH-WDD).
	Keycode string
	// Email is the license-owner email (lowercased lookup key server-side).
	Email string
	// LicenseKey is the long-lived license key. HMAC key only — never on
	// the wire.
	LicenseKey string
	// DeviceID is the stable per-install device id. Appears in the body
	// and in the X-Device-ID header.
	DeviceID string
	// Method is the uppercase HTTP method, typically "POST".
	Method string
	// Path is the request path with leading /v1/, no query string.
	Path string
	// Timestamp is unix seconds. Server tolerates 5 minutes of skew.
	Timestamp int64
}

// BootstrapSignature returns every intermediate so that conformance tests
// can assert each stage independently — if a future regression flips the
// SerializedBody, the failure points at exactly that stage instead of
// just "hex differs".
type BootstrapSignature struct {
	// SerializedBody is the JSON body exactly as sent on the wire. Bytes
	// signed verbatim.
	SerializedBody string
	// Canonical is `<ts>.<METHOD> <path>.<body>` — the HMAC input.
	Canonical string
	// Hex is lowercase hex HMAC-SHA256 over Canonical, keyed by
	// LicenseKey.
	Hex string
	// Header is the ISA-Signature header value: `t=<ts>,v1=<hex>`.
	Header string
}

// BuildBootstrapSignature reproduces the byte-exact signing flow defined
// in the auth-vector fixture. Returns an error if any required field is
// empty — the locked contract requires every field to be present.
func BuildBootstrapSignature(input BootstrapInput) (BootstrapSignature, error) {
	if input.Keycode == "" {
		return BootstrapSignature{}, errors.New("bootstrap signature: keycode is required")
	}
	if input.Email == "" {
		return BootstrapSignature{}, errors.New("bootstrap signature: email is required")
	}
	if input.LicenseKey == "" {
		return BootstrapSignature{}, errors.New("bootstrap signature: licenseKey is required")
	}
	if input.DeviceID == "" {
		return BootstrapSignature{}, errors.New("bootstrap signature: deviceId is required")
	}
	if input.Method == "" {
		return BootstrapSignature{}, errors.New("bootstrap signature: method is required")
	}
	if input.Path == "" {
		return BootstrapSignature{}, errors.New("bootstrap signature: path is required")
	}
	if input.Timestamp == 0 {
		return BootstrapSignature{}, errors.New("bootstrap signature: timestamp is required")
	}

	serializedBody := serializeBootstrapBody(input.Keycode, input.Email, input.DeviceID)
	canonical := buildBootstrapCanonical(input.Timestamp, input.Method, input.Path, serializedBody)
	mac := hmac.New(sha256.New, []byte(input.LicenseKey))
	mac.Write([]byte(canonical))
	digest := mac.Sum(nil)
	hexStr := hex.EncodeToString(digest)
	return BootstrapSignature{
		SerializedBody: serializedBody,
		Canonical:      canonical,
		Hex:            hexStr,
		Header:         "t=" + strconv.FormatInt(input.Timestamp, 10) + ",v1=" + hexStr,
	}, nil
}

// serializeBootstrapBody hand-rolls the JSON to pin key order and skip
// whitespace. encoding/json sorts struct fields by declaration order so a
// struct would also work, but a hand-rolled writer is unambiguous: any
// future field addition is a deliberate edit here, not an accidental
// reorder elsewhere.
func serializeBootstrapBody(keycode, email, deviceID string) string {
	var b strings.Builder
	b.Grow(64 + len(keycode) + len(email) + len(deviceID))
	b.WriteString(`{"keycode":`)
	writeJSONString(&b, keycode)
	b.WriteString(`,"email":`)
	writeJSONString(&b, email)
	b.WriteString(`,"deviceId":`)
	writeJSONString(&b, deviceID)
	b.WriteByte('}')
	return b.String()
}

// buildBootstrapCanonical mirrors the server-side
// buildBootstrapCanonical in services/account/internal/handler/sessions_bootstrap.go.
func buildBootstrapCanonical(ts int64, method, path, body string) string {
	return fmt.Sprintf("%d.%s %s.%s", ts, strings.ToUpper(method), path, body)
}

// writeJSONString emits a minimal RFC-8259 JSON string.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\u2028':
			b.WriteString(`\u2028`)
		case '\u2029':
			b.WriteString(`\u2029`)
		default:
			if r < 0x20 {
				const hexDigits = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[r>>4])
				b.WriteByte(hexDigits[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
