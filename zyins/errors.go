package zyins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrorCode is the stable wire enum returned by the ZyINS API. Callers
// switch on these values rather than HTTP status because status mapping
// is server policy that may evolve.
type ErrorCode string

const (
	// ErrorCodeUnspecified is the zero value; surfaced when the server
	// did not return a structured ProblemDetails body.
	ErrorCodeUnspecified ErrorCode = ""

	ErrorCodeUnauthorized        ErrorCode = "unauthorized"
	ErrorCodeTokenExpired        ErrorCode = "token_expired"
	ErrorCodeInvalidToken        ErrorCode = "invalid_token"
	ErrorCodeForbidden           ErrorCode = "forbidden"
	ErrorCodeNotFound            ErrorCode = "not_found"
	ErrorCodeConflict            ErrorCode = "conflict"
	ErrorCodeValidationError     ErrorCode = "validation_error"
	ErrorCodeLicenseLocked       ErrorCode = "license_locked"
	ErrorCodeLicenseInactive     ErrorCode = "license_inactive"
	ErrorCodeMaxActivations      ErrorCode = "max_activations"
	ErrorCodeActiveElsewhere     ErrorCode = "active_elsewhere"
	ErrorCodeIdempotencyConflict ErrorCode = "idempotency_conflict"
	ErrorCodeRateLimitExceeded   ErrorCode = "rate_limit_exceeded"
	ErrorCodeInternalError       ErrorCode = "internal_error"
	ErrorCodeBadGateway          ErrorCode = "bad_gateway"
	ErrorCodeGatewayTimeout      ErrorCode = "gateway_timeout"
	ErrorCodeServiceDown         ErrorCode = "service_unavailable"
)

// Error is the base error type all ZyINS API errors satisfy. Typed
// subclasses embed *Error so callers can either match the concrete
// type via errors.As, or read the common fields directly.
type Error struct {
	// Code is the stable wire enum. Callers MUST match on this rather
	// than on Message text — message strings are human-readable and may
	// change between releases.
	Code ErrorCode
	// Message is the human-readable detail. Empty when the server
	// returned only a code.
	Message string
	// HTTPStatus is the underlying HTTP status code; useful for
	// diagnostics but not for control flow.
	HTTPStatus int
	// Param names the request field that triggered a validation error.
	// Empty for non-validation errors.
	Param string
	// RequestID is the server's correlation identifier for the failed
	// request; surface this in logs and support tickets.
	RequestID string
	// DocURL points at the per-code remediation page when the server
	// supplies one.
	DocURL string
	// firstSeenAt is parsed from the problem-details body for
	// idempotency-conflict surfaces. Unexported because callers should
	// read it through IdempotencyConflictError.FirstSeenAt rather than
	// reaching into the base struct.
	firstSeenAt time.Time
}

// Error returns the human-readable message, or a synthesized fallback
// when the server returned only a code.
func (e *Error) Error() string {
	if e == nil {
		return "<nil zyins.Error>"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return fmt.Sprintf("zyins: %s (HTTP %d)", e.Code, e.HTTPStatus)
	}
	return fmt.Sprintf("zyins: HTTP %d", e.HTTPStatus)
}

// StatusCode returns the underlying HTTP status code. Consumers that
// must branch on status (e.g. mapping a 404 to a not-found sentinel)
// match this via an interface{ StatusCode() int } across the error
// chain rather than substring-scanning the message — a case id or body
// containing "404" must never be mistaken for a 404 response.
func (e *Error) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.HTTPStatus
}

// AuthError signals an authentication or authorization failure. The
// caller should rotate or reactivate the token rather than retry. Base
// carries the shared fields (code, message, request id).
type AuthError struct{ Base *Error }

// Error returns the underlying message; satisfies the error interface.
func (e *AuthError) Error() string { return e.Base.Error() }

// Unwrap exposes the base *Error so errors.As walks the chain.
func (e *AuthError) Unwrap() error { return e.Base }

// ValidationError signals a 4xx request-shape problem. Base.Param names
// the offending field when the server identified one.
type ValidationError struct{ Base *Error }

// Error returns the underlying message.
func (e *ValidationError) Error() string { return e.Base.Error() }

// Unwrap exposes the base *Error.
func (e *ValidationError) Unwrap() error { return e.Base }

// LicenseError signals a license-state problem (locked, inactive,
// activation cap reached). Different remediation per code.
type LicenseError struct{ Base *Error }

// Error returns the underlying message.
func (e *LicenseError) Error() string { return e.Base.Error() }

// Unwrap exposes the base *Error.
func (e *LicenseError) Unwrap() error { return e.Base }

// IdempotencyConflictError signals that the server has previously seen
// the Idempotency-Key under a different request body. Status 409, code
// idempotency_conflict. Key carries the conflicting key; FirstSeenAt
// carries the server-recorded original timestamp when supplied.
//
// Match with errors.As to surface the typed fields:
//
//	var ice *zyins.IdempotencyConflictError
//	if errors.As(err, &ice) {
//	    log.Printf("key %s first used at %s", ice.Key, ice.FirstSeenAt)
//	}
type IdempotencyConflictError struct {
	Base *Error
	// Key is the conflicting Idempotency-Key value.
	Key string
	// FirstSeenAt is the timestamp the server recorded for the original
	// request under this key. Zero when the server did not supply it.
	FirstSeenAt time.Time
}

// Error returns the underlying message.
func (e *IdempotencyConflictError) Error() string { return e.Base.Error() }

// Unwrap exposes the base *Error so errors.As walks the chain.
func (e *IdempotencyConflictError) Unwrap() error { return e.Base }

// IsaCode returns the stable wire enum, satisfying IsaError.
func (e *IdempotencyConflictError) IsaCode() ErrorCode { return ErrorCodeIdempotencyConflict }

// Is satisfies errors.Is for the ErrIdempotencyConflict sentinel.
func (e *IdempotencyConflictError) Is(target error) bool { return target == ErrIdempotencyConflict }

// RateLimitError signals a 429 from the server. RetryAfter carries the
// recommended wait, when the server supplied a Retry-After header.
type RateLimitError struct {
	Base *Error
	// RetryAfter is the duration the caller should wait before retrying.
	// Zero when the server did not supply a Retry-After header.
	RetryAfter time.Duration
}

// Error returns the underlying message.
func (e *RateLimitError) Error() string { return e.Base.Error() }

// Unwrap exposes the base *Error.
func (e *RateLimitError) Unwrap() error { return e.Base }

// Compile-time assertions: every typed error satisfies the standard
// library error interface. A future refactor that drops a method (e.g.
// renamed from Error() to ErrorString()) fails the build immediately.
var (
	_ error = (*Error)(nil)
	_ error = (*AuthError)(nil)
	_ error = (*ValidationError)(nil)
	_ error = (*LicenseError)(nil)
	_ error = (*RateLimitError)(nil)
	_ error = (*IdempotencyConflictError)(nil)

	// IsaError interface assertions — every typed error reports its
	// stable code so callers that match on the interface get a stable
	// shape across error variants.
	_ IsaError = (*Error)(nil)
	_ IsaError = (*AuthError)(nil)
	_ IsaError = (*ValidationError)(nil)
	_ IsaError = (*LicenseError)(nil)
	_ IsaError = (*RateLimitError)(nil)
	_ IsaError = (*IdempotencyConflictError)(nil)
)

// IsaCode returns e.Code so *Error satisfies IsaError directly.
func (e *Error) IsaCode() ErrorCode { return e.Code }

// IsaCode helpers for the typed subclasses — each forwards to the base.
func (e *AuthError) IsaCode() ErrorCode       { return e.Base.Code }
func (e *ValidationError) IsaCode() ErrorCode { return e.Base.Code }
func (e *LicenseError) IsaCode() ErrorCode    { return e.Base.Code }
func (e *RateLimitError) IsaCode() ErrorCode  { return e.Base.Code }

// Sentinel errors callers MAY match with errors.Is for the common
// classes. The fully-typed variants above expose richer fields; these
// are convenience handles for code that only needs to branch on class.
var (
	ErrAuth                = errors.New("zyins: authentication failed")
	ErrValidation          = errors.New("zyins: request validation failed")
	ErrLicense             = errors.New("zyins: license error")
	ErrRateLimit           = errors.New("zyins: rate limit exceeded")
	ErrIdempotencyConflict = errors.New("zyins: idempotency conflict")
)

// Is satisfies errors.Is for the typed subclasses so callers can write
// `errors.Is(err, zyins.ErrAuth)` without unwrapping.
func (e *AuthError) Is(target error) bool       { return target == ErrAuth }
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }
func (e *LicenseError) Is(target error) bool    { return target == ErrLicense }
func (e *RateLimitError) Is(target error) bool  { return target == ErrRateLimit }

// errorFromResponse parses a non-2xx HTTP response into the appropriate
// typed error. The response body is consumed; close is the caller's
// responsibility. Resolution order:
//
//  1. application/problem+json or JSON body with a `code` field → typed
//     subclass dispatch by code.
//  2. Legacy ERR_* token in the body → LicenseError with mapped code.
//  3. Fallback → base *Error with code ErrorCodeUnspecified.
//
// The caller always receives a typed value; malformed bodies do not
// yield nil errors.
func errorFromResponse(resp *http.Response) error {
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return &Error{
			HTTPStatus: resp.StatusCode,
			Message:    fmt.Sprintf("zyins: failed to read error body: %v", readErr),
			RequestID:  resp.Header.Get("X-Request-Id"),
		}
	}
	base := parseErrorBody(resp, body)
	return classify(base, resp)
}

// problemDetails mirrors the RFC 7807 envelope the API returns on 4xx
// and 5xx responses. first_seen_at is an idempotency-conflict
// extension; it is parsed eagerly so the typed error surface above can
// expose it as a time.Time without re-walking the body.
type problemDetails struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail"`
	Code        string `json:"code"`
	Param       string `json:"param"`
	DocURL      string `json:"doc_url"`
	Instance    string `json:"instance"`
	FirstSeenAt string `json:"first_seen_at"`
}

// pickFirstSeenAt prefers the response header
// (X-Idempotency-First-Seen-At), falling back to the timestamp parsed
// from the problem-details body. Header takes precedence so a
// malformed body never erases a clean header signal.
func pickFirstSeenAt(resp *http.Response, base *Error) time.Time {
	if hdr := resp.Header.Get("X-Idempotency-First-Seen-At"); hdr != "" {
		if t, err := time.Parse(time.RFC3339Nano, hdr); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, hdr); err == nil {
			return t
		}
	}
	if base != nil {
		return base.firstSeenAt
	}
	return time.Time{}
}

// parseErrorBody builds the base *Error from a response. ProblemDetails
// JSON takes precedence; legacy ERR_* strings fall through to the
// license-error mapper.
func parseErrorBody(resp *http.Response, body []byte) *Error {
	trimmed := strings.TrimSpace(string(body))
	base := &Error{
		HTTPStatus: resp.StatusCode,
		RequestID:  resp.Header.Get("X-Request-Id"),
		Message:    trimmed,
	}
	if strings.HasPrefix(trimmed, "{") {
		var pd problemDetails
		if err := json.Unmarshal(body, &pd); err == nil && (pd.Code != "" || pd.Detail != "" || pd.Title != "") {
			base.Code = ErrorCode(pd.Code)
			base.Param = pd.Param
			base.DocURL = pd.DocURL
			if pd.FirstSeenAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, pd.FirstSeenAt); err == nil {
					base.firstSeenAt = t
				} else if t, err := time.Parse(time.RFC3339, pd.FirstSeenAt); err == nil {
					base.firstSeenAt = t
				}
			}
			if pd.Detail != "" {
				base.Message = pd.Detail
			} else if pd.Title != "" {
				base.Message = pd.Title
			}
			return base
		}
	}
	if code, ok := legacyErrMap[trimmed]; ok {
		base.Code = code
		return base
	}
	return base
}

// classify wraps the base *Error in the matching typed subclass.
func classify(base *Error, resp *http.Response) error {
	switch base.Code {
	case ErrorCodeUnauthorized, ErrorCodeInvalidToken, ErrorCodeTokenExpired:
		return &AuthError{Base: base}
	case ErrorCodeValidationError:
		return &ValidationError{Base: base}
	case ErrorCodeLicenseLocked, ErrorCodeLicenseInactive,
		ErrorCodeMaxActivations, ErrorCodeActiveElsewhere:
		return &LicenseError{Base: base}
	case ErrorCodeRateLimitExceeded:
		return &RateLimitError{Base: base, RetryAfter: parseRetryAfter(resp)}
	case ErrorCodeIdempotencyConflict:
		return &IdempotencyConflictError{
			Base:        base,
			Key:         resp.Header.Get("Idempotency-Key"),
			FirstSeenAt: pickFirstSeenAt(resp, base),
		}
	}
	if resp.StatusCode == http.StatusConflict && resp.Header.Get("Idempotency-Key") != "" {
		// Servers that omit the structured code but echo the conflicting
		// key in the response header still surface as the typed error.
		base.Code = ErrorCodeIdempotencyConflict
		return &IdempotencyConflictError{
			Base:        base,
			Key:         resp.Header.Get("Idempotency-Key"),
			FirstSeenAt: pickFirstSeenAt(resp, base),
		}
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &AuthError{Base: base}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &ValidationError{Base: base}
	case http.StatusTooManyRequests:
		return &RateLimitError{Base: base, RetryAfter: parseRetryAfter(resp)}
	}
	return base
}

// legacyErrMap translates the legacy CGI's ERR_* magic strings to
// stable ErrorCode values so old endpoints surface in the modern typed
// hierarchy.
var legacyErrMap = map[string]ErrorCode{
	"ERR_MAX_ACTIVATIONS":     ErrorCodeMaxActivations,
	"ERR_INACTIVE":            ErrorCodeLicenseInactive,
	"ERR_ACTIVE_ELSEWHERE":    ErrorCodeActiveElsewhere,
	"ERR_LOCKED":              ErrorCodeLicenseLocked,
	"ERR_INVALID_CREDENTIALS": ErrorCodeInvalidToken,
	"NO_EMAIL":                ErrorCodeValidationError,
}

// parseRetryAfter decodes the Retry-After header per RFC 7231. Returns
// zero on absence or parse failure; callers fall back to exponential
// backoff in that case.
func parseRetryAfter(resp *http.Response) time.Duration {
	raw := resp.Header.Get("Retry-After")
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(raw); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
