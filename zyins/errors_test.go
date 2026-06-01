package zyins

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// buildResponse constructs an *http.Response for the error parser.
func buildResponse(status int, body string, headers map[string]string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
	for k, v := range headers {
		resp.Header.Set(k, v)
	}
	return resp
}

func TestErrorFromResponse_ProblemDetailsValidation(t *testing.T) {
	body := `{"code":"validation_error","detail":"email is required","param":"email","status":400}`
	err := errorFromResponse(buildResponse(http.StatusBadRequest, body, nil))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Base.Param != "email" {
		t.Errorf("Param = %q, want email", ve.Base.Param)
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("expected errors.Is to match ErrValidation")
	}
}

func TestErrorFromResponse_AuthOnHTTPStatus(t *testing.T) {
	err := errorFromResponse(buildResponse(http.StatusUnauthorized, "denied", nil))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrAuth) {
		t.Errorf("expected errors.Is(ErrAuth)")
	}
}

func TestErrorFromResponse_RateLimitWithRetryAfter(t *testing.T) {
	err := errorFromResponse(buildResponse(http.StatusTooManyRequests, "slow down", map[string]string{
		"Retry-After": "5",
	}))
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want 5s", rle.RetryAfter)
	}
}

func TestErrorFromResponse_LegacyErrToken(t *testing.T) {
	err := errorFromResponse(buildResponse(http.StatusForbidden, "ERR_LOCKED", nil))
	var le *LicenseError
	if !errors.As(err, &le) {
		t.Fatalf("expected *LicenseError, got %T: %v", err, err)
	}
	if le.Base.Code != ErrorCodeLicenseLocked {
		t.Errorf("Code = %q, want license_locked", le.Base.Code)
	}
}

func TestErrorFromResponse_FallbackKeepsBody(t *testing.T) {
	err := errorFromResponse(buildResponse(http.StatusInternalServerError, "boom", nil))
	if err == nil {
		t.Fatalf("expected an error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected body in error message; got %q", err.Error())
	}
}

func TestNilBaseError_DoesNotPanic(t *testing.T) {
	var e *Error
	if got := e.Error(); !strings.Contains(got, "nil") {
		t.Errorf("nil Error.Error() = %q", got)
	}
}
