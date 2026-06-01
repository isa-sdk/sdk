// Package proxy exposes `proxy.Call()` — structured invocation against
// the ISA Platform `/v1/call` endpoint, signed with canonical session-
// credential HMAC.
//
// Wire envelope (opaque pass-through; do NOT flatten):
//
//	{ integration_id | integration_uuid, method, params }
//
// Auth headers come from core.SignRequest (the canonical session
// signer); Idempotency-Key is auto-minted as a UUID v4 when the caller
// omits one. The SDK↔proxy hop is HMAC-signed; the proxy↔downstream
// hop remains Algosure HMAC and is handled server-side (ADR-035,
// amended in PR #<this>).
package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/isa-sdk/sdk/core"
	"github.com/isa-sdk/sdk/zyins"
)

// DefaultProxyOrigin is the production origin for the platform proxy.
const DefaultProxyOrigin = "https://proxy.isaapi.com"

const proxyCallPath = "/v1/call"

// defaultHTTPTimeout caps every outbound call when the caller does not
// supply their own *http.Client. Generous enough to cover an integration
// round-trip; tight enough to prevent a stuck proxy from leaking a
// connection.
const defaultHTTPTimeout = 30 * time.Second

// SessionBinding carries the credentials and origin a Call needs. Built
// by the parent Isa client at construction; tests build it directly.
type SessionBinding struct {
	SessionID     string
	SessionSecret string //nolint:gosec // documented credential field
	DeviceID      string
	ProxyOrigin   string
}

// CallOptions parameterize one invocation. Exactly one of
// IntegrationUUID / IntegrationID must be set; both or neither returns
// a *zyins.ValidationError before any network work.
type CallOptions struct {
	// IntegrationUUID is the preferred opaque identifier.
	IntegrationUUID string
	// IntegrationID is the legacy BIGSERIAL identifier. Must be positive when used.
	IntegrationID int64
	// Params is the opaque payload forwarded to the downstream integration.
	Params any
	// Method overrides the downstream HTTP method (default "POST").
	Method string
	// IdempotencyKey is auto-minted as a UUID v4 when empty.
	IdempotencyKey string

	// HTTPClient overrides the default client (test seam).
	HTTPClient *http.Client
	// Now pins the signing timestamp (test seam).
	Now time.Time
	// UUIDFactory overrides the default UUID v4 generator (test seam).
	UUIDFactory func() string
}

// Call invokes a registered integration through the platform proxy.
//
// Returns the raw response body bytes. Non-2xx responses are surfaced
// as typed errors:
//
//   - 401 → *zyins.AuthError
//   - 400 → *zyins.ValidationError
//   - 409 with code=idempotency_conflict → *zyins.IdempotencyConflictError
//   - everything else → *zyins.Error
func Call(ctx context.Context, b SessionBinding, opts CallOptions) ([]byte, error) {
	if err := validateBinding(b); err != nil {
		return nil, err
	}
	if err := validateIdentifier(opts); err != nil {
		return nil, err
	}
	body, err := buildEnvelope(opts)
	if err != nil {
		return nil, err
	}
	headers, err := signedHeaders(b, body, opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := newRequest(ctx, b.ProxyOrigin, body, headers)
	if err != nil {
		return nil, err
	}
	client := opts.HTTPClient
	if client == nil {
		// Fresh client with explicit timeout rather than http.DefaultClient
		// (no timeout, shared mutable singleton).
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("proxy.Call: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("proxy.Call: read response body: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	return nil, mapError(resp.StatusCode, raw)
}

func validateBinding(b SessionBinding) error {
	if b.SessionID == "" || b.SessionSecret == "" {
		return &zyins.ConfigError{
			Factory: "proxy.Call",
			Detail:  "proxy.Call requires a Session identity; exchange your bearer/license credentials via account.sessions.create first",
		}
	}
	return nil
}

func validateIdentifier(opts CallOptions) error {
	hasUUID := strings.TrimSpace(opts.IntegrationUUID) != ""
	hasID := opts.IntegrationID > 0
	if opts.IntegrationID < 0 {
		return validationErr(
			"proxy.Call: IntegrationID must be positive",
			"integration_id",
		)
	}
	if !hasUUID && !hasID {
		return validationErr(
			"proxy.Call: IntegrationID must be positive when IntegrationUUID is empty",
			"integration_id",
		)
	}
	if hasUUID && hasID {
		return validationErr(
			"proxy.Call: supply exactly one of IntegrationUUID or IntegrationID",
			"integration_uuid",
		)
	}
	return nil
}

// buildEnvelope serializes the request envelope deterministically.
// Bytes hashed for signing MUST equal bytes on the wire — JSON
// non-determinism here would break the HMAC.
func buildEnvelope(opts CallOptions) ([]byte, error) {
	envelope := map[string]any{}
	if strings.TrimSpace(opts.IntegrationUUID) != "" {
		envelope["integration_uuid"] = opts.IntegrationUUID
	} else {
		envelope["integration_id"] = opts.IntegrationID
	}
	method := opts.Method
	if method == "" {
		method = http.MethodPost
	}
	envelope["method"] = method
	envelope["params"] = opts.Params
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("proxy.Call: marshal envelope: %w", err)
	}
	return body, nil
}

func signedHeaders(b SessionBinding, body []byte, opts CallOptions) (map[string]string, error) {
	signed, err := core.SignRequest(core.SignRequestInput{
		Method:        http.MethodPost,
		Path:          proxyCallPath,
		Body:          body,
		SessionID:     b.SessionID,
		SessionSecret: b.SessionSecret,
		DeviceID:      b.DeviceID,
		Now:           opts.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("proxy.Call: sign request: %w", err)
	}
	out := signed.AsMap()
	out["Content-Type"] = "application/json"
	if opts.IdempotencyKey != "" {
		out["Idempotency-Key"] = opts.IdempotencyKey
	} else if opts.UUIDFactory != nil {
		out["Idempotency-Key"] = opts.UUIDFactory()
	} else {
		out["Idempotency-Key"] = mintUUIDv4()
	}
	return out, nil
}

func newRequest(ctx context.Context, origin string, body []byte, headers map[string]string) (*http.Request, error) {
	url := strings.TrimRight(origin, "/") + proxyCallPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("proxy.Call: build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// mintUUIDv4 returns a UUID v4 string. Uses crypto/rand directly so the
// proxy package adds no new runtime dependencies.
func mintUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read never fails on a sane runtime; the fallback
		// uses a fixed-time-based suffix so callers never see a panic.
		return fallbackUUIDv4(time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

const fallbackUUIDNodeMask = 0xFFFFFFFFFFFF

func fallbackUUIDv4(now int64) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012x", now&fallbackUUIDNodeMask)
}

func validationErr(message, param string) error {
	return &zyins.ValidationError{Base: &zyins.Error{
		Message:    message,
		Code:       zyins.ErrorCodeValidationError,
		HTTPStatus: http.StatusBadRequest,
		Param:      param,
	}}
}
