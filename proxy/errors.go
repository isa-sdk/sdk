// Error mapping for proxy.Call responses.
//
// Resolves non-2xx HTTP statuses + the server's ProblemDetails body
// into the typed zyins.* error hierarchy. Centralizes the funnel so
// every call site sees the same shape.

package proxy

import (
	"encoding/json"
	"time"

	"github.com/isa-sdk/sdk/zyins"
)

// problemDetails is the wire shape of the server's error envelope.
// Mirrors RFC 7807 with ISA Platform extensions (code, advice_code,
// request_id, key, first_seen_at).
type problemDetails struct {
	Code        string `json:"code"`
	Detail      string `json:"detail"`
	Message     string `json:"message"`
	Title       string `json:"title"`
	Param       string `json:"param"`
	RequestID   string `json:"request_id"`
	DocURL      string `json:"doc_url"`
	AdviceCode  string `json:"advice_code"`
	Key         string `json:"key"`
	FirstSeenAt string `json:"first_seen_at"`
}

// mapError builds a typed *zyins.* error from an HTTP status + body.
func mapError(status int, raw []byte) error {
	pd := parseProblem(raw)
	base := &zyins.Error{
		Code:       zyins.ErrorCode(orDefault(pd.Code, "api_error")),
		Message:    pickMessage(pd, raw),
		HTTPStatus: status,
		Param:      pd.Param,
		RequestID:  pd.RequestID,
		DocURL:     pd.DocURL,
	}
	switch {
	case status == 401:
		base.Code = zyins.ErrorCode(orDefault(pd.Code, "unauthorized"))
		return &zyins.AuthError{Base: base}
	case status == 400:
		base.Code = zyins.ErrorCode(orDefault(pd.Code, string(zyins.ErrorCodeValidationError)))
		return &zyins.ValidationError{Base: base}
	case status == 409 && pd.Code == string(zyins.ErrorCodeIdempotencyConflict):
		return &zyins.IdempotencyConflictError{
			Base:        base,
			Key:         pd.Key,
			FirstSeenAt: parseTimestamp(pd.FirstSeenAt),
		}
	}
	return base
}

func parseProblem(raw []byte) problemDetails {
	var pd problemDetails
	if len(raw) == 0 {
		return pd
	}
	_ = json.Unmarshal(raw, &pd)
	return pd
}

func pickMessage(pd problemDetails, raw []byte) string {
	if pd.Detail != "" {
		return pd.Detail
	}
	if pd.Message != "" {
		return pd.Message
	}
	if pd.Title != "" {
		return pd.Title
	}
	if len(raw) > 0 {
		return string(raw)
	}
	return "proxy.Call: non-2xx response with empty body"
}

func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
