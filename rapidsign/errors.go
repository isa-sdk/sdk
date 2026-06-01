package rapidsign

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrorCode is the stable wire enum from api.isa.v1.ErrorCode. Clients
// switch on these values rather than HTTP status because the server's
// status mapping is policy that may evolve.
type ErrorCode string

// Known error codes. The set mirrors api.isa.v1.ErrorCode at the time
// of writing; unknown codes are surfaced as base *Error with the raw
// code preserved.
const (
	ErrorCodeUnspecified        ErrorCode = ""
	ErrorCodeUnauthorized       ErrorCode = "unauthorized"
	ErrorCodeTokenExpired       ErrorCode = "token_expired"
	ErrorCodeInvalidToken       ErrorCode = "invalid_token"
	ErrorCodeForbidden          ErrorCode = "forbidden"
	ErrorCodeNotFound           ErrorCode = "not_found"
	ErrorCodeMethodNotAllowed   ErrorCode = "method_not_allowed"
	ErrorCodeConflict           ErrorCode = "conflict"
	ErrorCodeValidationError    ErrorCode = "validation_error"
	ErrorCodeLicenseLocked      ErrorCode = "license_locked"
	ErrorCodeRateLimitExceeded  ErrorCode = "rate_limit_exceeded"
	ErrorCodeInternalError      ErrorCode = "internal_error"
	ErrorCodeBadGateway         ErrorCode = "bad_gateway"
	ErrorCodeGatewayTimeout     ErrorCode = "gateway_timeout"
	ErrorCodeServiceUnavailable ErrorCode = "service_unavailable"
	ErrorCodeNotImplemented     ErrorCode = "not_implemented"
)

// Error is the base error type all rapidsign API errors satisfy. The
// typed subclasses below embed *Error so callers can either match on
// the concrete type via errors.As, or read the common fields directly.
type Error struct {
	Code       ErrorCode
	HTTPStatus int
	RequestID  string
	Detail     string
	Retryable  bool
	RetryAfter time.Duration
}

// Error returns the human-readable detail message. Empty when the
// server returned no detail; callers should rely on Code, HTTPStatus,
// or RequestID for programmatic handling.
func (e *Error) Error() string {
	if e == nil {
		return "<nil rapidsign.Error>"
	}
	if e.Detail != "" {
		return e.Detail
	}
	if e.Code != "" {
		return fmt.Sprintf("rapidsign: %s (http %d)", e.Code, e.HTTPStatus)
	}
	return fmt.Sprintf("rapidsign: http %d", e.HTTPStatus)
}

// UnauthorizedError wraps ERROR_CODE_UNAUTHORIZED / 401 responses.
type UnauthorizedError struct{ Err *Error }

// Error forwards to the embedded base error so the typed subclass
// satisfies the error interface (the embedded field's `Error` name
// shadows the method).
func (e *UnauthorizedError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error so errors.As/errors.Is can climb to
// the underlying value when callers prefer matching on the common type.
func (e *UnauthorizedError) Unwrap() error { return e.Err }

// TokenExpiredError wraps ERROR_CODE_TOKEN_EXPIRED.
type TokenExpiredError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *TokenExpiredError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *TokenExpiredError) Unwrap() error { return e.Err }

// InvalidTokenError wraps ERROR_CODE_INVALID_TOKEN.
type InvalidTokenError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *InvalidTokenError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *InvalidTokenError) Unwrap() error { return e.Err }

// ForbiddenError wraps 403 responses.
type ForbiddenError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *ForbiddenError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *ForbiddenError) Unwrap() error { return e.Err }

// NotFoundError wraps 404 responses.
type NotFoundError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *NotFoundError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *NotFoundError) Unwrap() error { return e.Err }

// ConflictError wraps 409 responses (idempotency mismatch, sign_id
// already used, etc.).
type ConflictError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *ConflictError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *ConflictError) Unwrap() error { return e.Err }

// ValidationError wraps 400 / ERROR_CODE_VALIDATION_ERROR responses.
// Field is set when the server identified a single offending field via
// the problem-details `param` extension.
type ValidationError struct {
	Err   *Error
	Field string
}

// Error satisfies the error interface; see UnauthorizedError.Error.
// When a Field is present it is appended to the detail for human
// readability (`<detail> (field=<name>)`).
func (e *ValidationError) Error() string {
	base := e.Err.Error()
	if e.Field != "" {
		return base + " (field=" + e.Field + ")"
	}
	return base
}

// Unwrap exposes the base *Error for errors.As traversal.
func (e *ValidationError) Unwrap() error { return e.Err }

// RateLimitedError wraps 429 responses. RetryAfter is populated from
// Retry-After when present (delta-seconds or HTTP-date); zero when
// absent.
type RateLimitedError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *RateLimitedError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *RateLimitedError) Unwrap() error { return e.Err }

// ServiceUnavailableError wraps 503 / ERROR_CODE_SERVICE_UNAVAILABLE.
type ServiceUnavailableError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *ServiceUnavailableError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *ServiceUnavailableError) Unwrap() error { return e.Err }

// NotImplementedError is returned for surfaces the SDK exposes but the
// server has not yet shipped (e.g. Documents.Cancel today).
type NotImplementedError struct{ Err *Error }

// Error satisfies the error interface; see UnauthorizedError.Error.
func (e *NotImplementedError) Error() string { return e.Err.Error() }

// Unwrap exposes the base *Error for errors.As traversal.
func (e *NotImplementedError) Unwrap() error { return e.Err }

// problemDetails is the RFC 7807 body the server emits. The `code` and
// `param` extensions are conventions from api-standards.md.
type problemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
	Code     string `json:"code"`
	Param    string `json:"param"`
}

// parseErrorResponse reads resp.Body and returns the typed error that
// best describes it. The HTTP body is consumed unconditionally so the
// caller does not have to.
//
// classification precedence: explicit ProblemDetails.Code > HTTP status
// fallback. Unknown codes return *Error (base type) so consumers can
// still inspect Code/HTTPStatus. now() is injected so Retry-After
// HTTP-date math is testable without freezing wall-clock time.
func parseErrorResponse(resp *http.Response, now time.Time) error {
	defer func() {
		// Drain so connection can be reused; ignore errors — we already
		// have the body we needed.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = resp.Header.Get("Request-Id")
	}
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), now)

	base := &Error{
		HTTPStatus: resp.StatusCode,
		RequestID:  requestID,
		RetryAfter: retryAfter,
		Retryable:  retryAfter > 0 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
	}

	if len(body) > 0 && strings.Contains(resp.Header.Get("Content-Type"), "json") {
		var pd problemDetails
		if err := json.Unmarshal(body, &pd); err == nil {
			base.Code = ErrorCode(pd.Code)
			base.Detail = pd.Detail
			if pd.Detail == "" {
				base.Detail = pd.Title
			}
			return classify(base, pd.Param)
		}
	}

	// No / unparseable body — classify from status alone.
	if base.Detail == "" {
		base.Detail = fmt.Sprintf("rapidsign: unexpected http %d", resp.StatusCode)
	}
	return classify(base, "")
}

// classify maps a populated *Error to the typed subclass best
// describing it. Code wins when present; otherwise HTTP status
// determines the subclass. Unknown codes return the base *Error.
func classify(base *Error, field string) error {
	switch base.Code {
	case ErrorCodeUnauthorized:
		return &UnauthorizedError{Err: base}
	case ErrorCodeTokenExpired:
		return &TokenExpiredError{Err: base}
	case ErrorCodeInvalidToken:
		return &InvalidTokenError{Err: base}
	case ErrorCodeForbidden:
		return &ForbiddenError{Err: base}
	case ErrorCodeNotFound:
		return &NotFoundError{Err: base}
	case ErrorCodeConflict:
		return &ConflictError{Err: base}
	case ErrorCodeValidationError:
		return &ValidationError{Err: base, Field: field}
	case ErrorCodeRateLimitExceeded:
		return &RateLimitedError{Err: base}
	case ErrorCodeServiceUnavailable:
		return &ServiceUnavailableError{Err: base}
	case ErrorCodeNotImplemented:
		return &NotImplementedError{Err: base}
	case ErrorCodeUnspecified:
		// Fall through to HTTP-status mapping when the server omitted code.
	default:
		if base.Code != "" {
			return base
		}
	}

	switch base.HTTPStatus {
	case http.StatusUnauthorized:
		return &UnauthorizedError{Err: base}
	case http.StatusForbidden:
		return &ForbiddenError{Err: base}
	case http.StatusNotFound:
		return &NotFoundError{Err: base}
	case http.StatusConflict:
		return &ConflictError{Err: base}
	case http.StatusBadRequest:
		return &ValidationError{Err: base, Field: field}
	case http.StatusTooManyRequests:
		return &RateLimitedError{Err: base}
	case http.StatusServiceUnavailable:
		return &ServiceUnavailableError{Err: base}
	case http.StatusNotImplemented:
		return &NotImplementedError{Err: base}
	}
	return base
}

// parseRetryAfter handles both RFC 7231 forms: delta-seconds and
// HTTP-date. Returns zero on parse failure so the caller falls back to
// its own backoff strategy. now is supplied by the caller so HTTP-date
// math is deterministic under test.
func parseRetryAfter(raw string, now time.Time) time.Duration {
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(raw); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
