package zyins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Paths for the ZyINS public license-lifecycle surface (see ADR-013).
//
// These three operations sit OUTSIDE AuthMiddleware on the server: activate
// is the call that MINTS the licenseKey, so the SDK cannot sign requests
// with a credential it does not yet have. The wire body uses camelCase
// (`deviceId`) and the response is wrapped in the platform envelope.
const (
	licensesActivatePath   = "/v2/licenses/activate"
	licensesCheckPath      = "/v2/licenses/check"
	licensesDeactivatePath = "/v2/licenses/deactivate"
)

// LicenseValidationStatus mirrors the proto `LicenseStatus` enum
// (api.zyins.v1.LicenseStatus). Wire values are lower-case strings.
type LicenseValidationStatus string

// Enumerated license validation states. Unknown wire values are
// surfaced verbatim so the SDK never drops information.
const (
	LicenseStatusValid    LicenseValidationStatus = "valid"
	LicenseStatusInvalid  LicenseValidationStatus = "invalid"
	LicenseStatusInactive LicenseValidationStatus = "inactive"
)

// LicenseService groups the public BPP license-lifecycle calls
// (Activate, Check, Deactivate). Authenticated self-* operations
// (LockSelf, RefreshSelf, etc.) ship after the LicenseHMAC transport
// lands; this service exposes the no-auth + body-credential surface
// only.
type LicenseService struct {
	client *Client
	// state is the optional CredentialState attached via WithState. When
	// non-nil, zero-arg ergonomic variants (Activate / Check / Deactivate
	// without an explicit input) pull credentials from it.
	state *CredentialState
}

// WithState attaches a CredentialState to the service so zero-arg
// activate/check/deactivate variants can pull credentials and stash the
// minted license key back into shared state automatically.
func (s *LicenseService) WithState(state *CredentialState) *LicenseService {
	s.state = state
	return s
}

// State returns the attached CredentialState, or nil when none is set.
func (s *LicenseService) State() *CredentialState { return s.state }

// LicenseCheckInput is the typed request shape for License.Check.
type LicenseCheckInput struct {
	// Email associated with the license. Required.
	Email string
	// Keycode is the BPP order keycode (XXX-XXX-XXX, case-insensitive).
	// Required.
	Keycode string
	// DeviceID is the client-generated device fingerprint. Optional;
	// when supplied, the server includes it in the anti-piracy check.
	DeviceID string
	// LicenseKey is the deterministic license key to verify. Optional;
	// when supplied, the server validates it against the order.
	LicenseKey string
}

// LicenseCheckResult is the typed response from License.Check.
type LicenseCheckResult struct {
	// Status is the validation outcome (valid, invalid, inactive).
	Status LicenseValidationStatus `json:"status"`
}

// LicenseDeactivateInput is the typed request shape for
// License.Deactivate.
type LicenseDeactivateInput struct {
	// Email associated with the license. Required.
	Email string
	// Keycode is the BPP order keycode. Required.
	Keycode string
	// DeviceID is the device fingerprint. Optional; reset on success.
	DeviceID string
}

// LicenseDeactivateResult is the typed response from
// License.Deactivate.
type LicenseDeactivateResult struct {
	// Status is "inactive" on success against the v2 surface; the
	// legacy v1 wire word "deactivated" is preserved verbatim when the
	// server still serves it.
	Status string `json:"status"`
	// RemainingActivations reflects the order's activation slots after
	// the deactivate. A nil value means the server did not return the
	// field, as with legacy v1 success bodies.
	RemainingActivations *int `json:"remaining_activations,omitempty"`
}

// licensesCheckWireBody is the on-wire JSON body for /v2/licenses/check.
// The v2 surface uses camelCase keys; mirror the TS SDK exactly.
type licensesCheckWireBody struct {
	Email      string `json:"email"`
	Keycode    string `json:"keycode"`
	DeviceID   string `json:"deviceId,omitempty"`
	LicenseKey string `json:"licenseKey,omitempty"`
}

// licensesDeactivateWireBody is the on-wire JSON body for
// /v2/licenses/deactivate.
type licensesDeactivateWireBody struct {
	Email    string `json:"email"`
	Keycode  string `json:"keycode"`
	DeviceID string `json:"deviceId,omitempty"`
}

// Check performs a lightweight phone-home validation. Mounted outside
// AuthMiddleware on the server; the SDK strips the Authorization header
// for this call so the client struct can be reused for every operation
// without leaking a stale or pre-bootstrap credential into the request.
func (s *LicenseService) Check(ctx context.Context, input *LicenseCheckInput, opts ...RunOption) (*LicenseCheckResult, error) {
	filled := s.fillCheck(input)
	if strings.TrimSpace(filled.Email) == "" {
		return nil, validationFailure("zyins: LicenseCheckInput.Email is required")
	}
	if strings.TrimSpace(filled.Keycode) == "" {
		return nil, validationFailure("zyins: LicenseCheckInput.Keycode is required")
	}
	deviceID := strings.TrimSpace(filled.DeviceID)
	body := licensesCheckWireBody{
		Email:      filled.Email,
		Keycode:    filled.Keycode,
		DeviceID:   deviceID,
		LicenseKey: filled.LicenseKey,
	}
	ro := collectRunOptions(opts)
	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           licensesCheckPath,
		body:           body,
		op:             "licenses_check",
		idempotencyKey: ro.idempotencyKey,
		bootstrap:      true,
		extraHeaders:   bootstrapHeaders(deviceID),
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: License.Check: %w", err)
	}
	return decodeLicenseCheckResponse(raw)
}

// Deactivate revokes an activation, resets the anti-piracy device
// record, and marks the order inactive. Mounted outside AuthMiddleware;
// the SDK strips the Authorization header for the same reason as Check.
func (s *LicenseService) Deactivate(ctx context.Context, input *LicenseDeactivateInput, opts ...RunOption) (*LicenseDeactivateResult, error) {
	filled := s.fillDeactivate(input)
	if strings.TrimSpace(filled.Email) == "" {
		return nil, validationFailure("zyins: LicenseDeactivateInput.Email is required")
	}
	if strings.TrimSpace(filled.Keycode) == "" {
		return nil, validationFailure("zyins: LicenseDeactivateInput.Keycode is required")
	}
	deviceID := strings.TrimSpace(filled.DeviceID)
	body := licensesDeactivateWireBody{
		Email:    filled.Email,
		Keycode:  filled.Keycode,
		DeviceID: deviceID,
	}
	ro := collectRunOptions(opts)
	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           licensesDeactivatePath,
		body:           body,
		op:             "licenses_deactivate",
		idempotencyKey: ro.idempotencyKey,
		bootstrap:      true,
		extraHeaders:   bootstrapHeaders(deviceID),
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: License.Deactivate: %w", err)
	}
	result, err := decodeLicenseDeactivateResponse(raw)
	if err != nil {
		return nil, err
	}
	if s.state != nil {
		if err := s.state.ClearLicenseKey(); err != nil {
			return nil, fmt.Errorf("zyins: License.Deactivate clearing state: %w", err)
		}
	}
	return result, nil
}

// fillCheck merges the caller-supplied input with the attached state.
func (s *LicenseService) fillCheck(input *LicenseCheckInput) LicenseCheckInput {
	out := LicenseCheckInput{}
	if input != nil {
		out = *input
	}
	if s.state == nil {
		return out
	}
	snap := s.state.Snapshot()
	if out.Email == "" {
		out.Email = snap.Email
	}
	if out.Keycode == "" {
		out.Keycode = snap.Keycode
	}
	if out.DeviceID == "" {
		out.DeviceID = snap.DeviceID
	}
	if out.LicenseKey == "" {
		out.LicenseKey = snap.LicenseKey
	}
	return out
}

// fillDeactivate merges the caller-supplied input with the attached state.
func (s *LicenseService) fillDeactivate(input *LicenseDeactivateInput) LicenseDeactivateInput {
	out := LicenseDeactivateInput{}
	if input != nil {
		out = *input
	}
	if s.state == nil {
		return out
	}
	snap := s.state.Snapshot()
	if out.Email == "" {
		out.Email = snap.Email
	}
	if out.Keycode == "" {
		out.Keycode = snap.Keycode
	}
	if out.DeviceID == "" {
		out.DeviceID = snap.DeviceID
	}
	return out
}

// bootstrapHeaders returns the only non-canonical header the v2 license
// surface accepts: X-Device-ID. Empty deviceId yields a nil map so the
// transport skips the header entirely.
func bootstrapHeaders(deviceID string) map[string]string {
	trimmed := strings.TrimSpace(deviceID)
	if trimmed == "" {
		return nil
	}
	return map[string]string{"X-Device-ID": trimmed}
}

// collectRunOptions folds opts into a single runOptions value. Defined
// here so future per-call options can be added without touching every
// sub-service.
func collectRunOptions(opts []RunOption) runOptions {
	ro := runOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}
	return ro
}

// validationFailure produces a typed *ValidationError. Identical to
// the construction pattern used in Prequalify.Run; extracted so every
// sub-service speaks the same error idiom.
func validationFailure(msg string) error {
	return &ValidationError{Base: &Error{
		Code:    ErrorCodeValidationError,
		Message: msg,
	}}
}

// decodeLicenseCheckResponse parses the /v2/licenses/check 200 body.
// Tolerates both the bare proto shape and the platform ADR-012
// envelope `{ data: { ... } }` so a future server-side wrap does not
// break clients.
func decodeLicenseCheckResponse(body []byte) (*LicenseCheckResult, error) {
	data, err := unwrapEnvelope(body, "licenses_check")
	if err != nil {
		return nil, err
	}
	var result LicenseCheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode licenses_check response: %w", err)
	}
	return &result, nil
}

// decodeLicenseDeactivateResponse parses the /v2/licenses/deactivate
// 200 body, applying the same envelope tolerance as Check. The v2
// server returns `{status:"inactive", remainingActivations}`; legacy
// `{status:"deactivated"}` is preserved verbatim for callers still
// targeting the v1 wire.
func decodeLicenseDeactivateResponse(body []byte) (*LicenseDeactivateResult, error) {
	data, err := unwrapEnvelope(body, "licenses_deactivate")
	if err != nil {
		return nil, err
	}
	var wire struct {
		Status               string `json:"status"`
		RemainingActivations *int   `json:"remainingActivations"`
		// LegacyRemaining accepts the snake_case spelling so a server
		// still serving the v1 wire word does not drop the counter.
		LegacyRemaining *int `json:"remaining_activations"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode licenses_deactivate response: %w", err)
	}
	var remaining *int
	if wire.RemainingActivations != nil {
		remaining = wire.RemainingActivations
	} else if wire.LegacyRemaining != nil {
		remaining = wire.LegacyRemaining
	}
	return &LicenseDeactivateResult{
		Status:               wire.Status,
		RemainingActivations: remaining,
	}, nil
}

// unwrapEnvelope returns the inner data bytes for an ADR-012 response,
// or the full body when no envelope is present.
func unwrapEnvelope(body []byte, op string) ([]byte, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("zyins: %s response body was empty", op)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode %s envelope: %w", op, err)
	}
	if data, ok := env["data"]; ok {
		if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return nil, fmt.Errorf("zyins: %s envelope data was null", op)
		}
		return data, nil
	}
	return body, nil
}
