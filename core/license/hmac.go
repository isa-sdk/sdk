// Package license provides the License-HMAC request signing primitives
// that bind a license identity to a per-request signature.
//
// Wire contract (server side: go/zyins/server/auth_middleware.go +
// go/zyins/server/device_signature.go):
//
//	Authorization:        License base64("<licenseKey>:<orderId>:<email>")
//	X-Device-ID:          <deviceId>
//	X-Device-Signature:   hex(HMAC-SHA256(deviceId, canonical))
//	X-License-Method:     <METHOD>
//	X-License-URI:        <path[?query]>
//	X-License-Timestamp:  <unix-ms>
//
// The canonical string signed under the device id is:
//
//	<METHOD>\n<requestURI>\n<timestamp>\n<body>
//
// The TS SDK's buildLicenseHMACHeaders is the reference; this is the
// byte-identical Go translation.
package license

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// HeaderAuthorization and friends are the canonical header names emitted
// by Build. Exported so callers do not rebuild the literal in every
// transport.
const (
	HeaderAuthorization      = "Authorization"
	HeaderDeviceID           = "X-Device-ID"
	HeaderDeviceSignature    = "X-Device-Signature"
	HeaderLicenseMethod      = "X-License-Method"
	HeaderLicenseURI         = "X-License-URI"
	HeaderLicenseTimestamp   = "X-License-Timestamp"
	authorizationLicenseTag  = "License"
)

// Errors surfaced by Build.
var (
	// ErrMissingLicenseKey fires when the caller did not supply a license
	// key. The server rejects empty-key requests with 401, but failing
	// fast at sign time gives a much better error.
	ErrMissingLicenseKey = errors.New("license: licenseKey is required")
	// ErrMissingOrderID fires when orderId is empty. The server requires
	// orderId in the License authorization payload.
	ErrMissingOrderID = errors.New("license: orderId is required")
	// ErrMissingEmail fires when email is empty.
	ErrMissingEmail = errors.New("license: email is required")
	// ErrMissingDeviceID fires when deviceId is empty. The signature has
	// no key without it.
	ErrMissingDeviceID = errors.New("license: deviceId is required")
	// ErrMissingMethod fires when the HTTP method is empty.
	ErrMissingMethod = errors.New("license: method is required")
	// ErrMissingRequestURI fires when the request URI is empty.
	ErrMissingRequestURI = errors.New("license: requestURI is required")
)

// Clock returns the current instant; defaults to time.Now. Tests inject
// a deterministic clock so signature outputs are reproducible.
type Clock func() time.Time

// Input groups the fields needed to compute a License-HMAC header bundle.
// Empty Now falls back to time.Now().UTC(); empty Clock is honored only
// when Now is also zero.
type Input struct {
	LicenseKey string
	OrderID    string
	Email      string
	Method     string
	RequestURI string
	// Body is the raw request body; empty for GET/HEAD calls. The bytes
	// are signed verbatim — pre-serialize JSON before passing.
	Body     []byte
	DeviceID string
	// Clock injects a deterministic timestamp source. nil → time.Now.
	Clock Clock
}

// Headers is the six-header bundle emitted by Build. AsMap renders the
// canonical wire form for transport layers.
type Headers struct {
	Authorization     string
	DeviceID          string
	DeviceSignature   string
	LicenseMethod     string
	LicenseURI        string
	LicenseTimestamp  string
}

// AsMap returns the header bundle as a map keyed by canonical names.
// Iteration order is not stable; callers MUST NOT depend on it.
func (h Headers) AsMap() map[string]string {
	return map[string]string{
		HeaderAuthorization:    h.Authorization,
		HeaderDeviceID:         h.DeviceID,
		HeaderDeviceSignature:  h.DeviceSignature,
		HeaderLicenseMethod:    h.LicenseMethod,
		HeaderLicenseURI:       h.LicenseURI,
		HeaderLicenseTimestamp: h.LicenseTimestamp,
	}
}

// StripQuotes removes a single pair of surrounding double-quote
// characters. AsyncStorage round-trips on iOS occasionally leave a
// JSON-quoted value behind; matches the TS helper of the same name.
func StripQuotes(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

// Build computes the License-HMAC header bundle.
func Build(in Input) (Headers, error) {
	if strings.TrimSpace(in.LicenseKey) == "" {
		return Headers{}, ErrMissingLicenseKey
	}
	if strings.TrimSpace(in.OrderID) == "" {
		return Headers{}, ErrMissingOrderID
	}
	if strings.TrimSpace(in.Email) == "" {
		return Headers{}, ErrMissingEmail
	}
	if strings.TrimSpace(in.DeviceID) == "" {
		return Headers{}, ErrMissingDeviceID
	}
	if strings.TrimSpace(in.Method) == "" {
		return Headers{}, ErrMissingMethod
	}
	if strings.TrimSpace(in.RequestURI) == "" {
		return Headers{}, ErrMissingRequestURI
	}

	clock := in.Clock
	if clock == nil {
		clock = time.Now
	}
	// TS uses Date.now() (unix ms); mirror exactly.
	ts := strconv.FormatInt(clock().UTC().UnixMilli(), 10)
	canonical := in.Method + "\n" + in.RequestURI + "\n" + ts + "\n" + string(in.Body)
	deviceID := StripQuotes(in.DeviceID)
	mac := hmac.New(sha256.New, []byte(deviceID))
	mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))
	payload := StripQuotes(in.LicenseKey) + ":" + StripQuotes(in.OrderID) + ":" + StripQuotes(in.Email)
	authz := authorizationLicenseTag + " " + base64.StdEncoding.EncodeToString([]byte(payload))
	return Headers{
		Authorization:    authz,
		DeviceID:         deviceID,
		DeviceSignature:  sig,
		LicenseMethod:    in.Method,
		LicenseURI:       in.RequestURI,
		LicenseTimestamp: ts,
	}, nil
}
