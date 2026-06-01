// Envelope, RawResponse, and the WithRawResponse operation variants
// that expose underlying HTTP metadata to callers that need it.
//
// The default operation surface (e.g. Prequalify.Run) returns the
// parsed typed result. WithRawResponse counterparts return the same
// parsed body PLUS a RawResponse handle carrying status, headers, and
// URL. The variant pattern matches Stainless/OpenAI/Anthropic SDK
// convention so a developer fluent in one is fluent in all.

package zyins

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// envelopeMeta mirrors the cross-cutting fields the server emits on
// every response. Kept private so callers do not depend on the JSON
// shape; the public Envelope[T] surfaces these as named fields.
type envelopeMeta struct {
	RequestID      string `json:"request_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Livemode       bool   `json:"livemode"`
	RetryAttempts  int    `json:"retry_attempts"`
}

// newEnvelope builds an Envelope[T] from a decoded typed result plus
// the raw body and the underlying HTTP response. Envelope metadata is
// read from the JSON body when present, falling back to response
// headers and the request's outbound Idempotency-Key when the server
// elides those fields.
func newEnvelope[T any](data T, body []byte, resp *http.Response) *Envelope[T] {
	env := &Envelope[T]{Data: data}
	if len(body) > 0 {
		var meta envelopeMeta
		if err := json.Unmarshal(body, &meta); err == nil {
			env.RequestID = meta.RequestID
			env.IdempotencyKey = meta.IdempotencyKey
			env.Livemode = meta.Livemode
			env.RetryAttempts = meta.RetryAttempts
		}
	}
	if resp != nil {
		if env.RequestID == "" {
			env.RequestID = resp.Header.Get("X-Request-Id")
		}
		if env.IdempotencyKey == "" && resp.Request != nil {
			env.IdempotencyKey = resp.Request.Header.Get("Idempotency-Key")
		}
	}
	return env
}

// Envelope wraps a typed result with the cross-cutting fields every
// response carries per ADR-012. Generics here let one type serve every
// operation without per-operation envelope structs.
//
//	env, err := client.Prequalify.RunEnvelope(ctx, input)
//	env.Data            // *PrequalifyResult
//	env.RequestID       // server correlation id
//	env.IdempotencyKey  // SDK-minted or caller-overridden
//	env.RetryAttempts   // 0 for first-try success
type Envelope[T any] struct {
	// Data is the typed payload parsed from the JSON body. Pointer or
	// value depends on the operation; both are valid type parameters.
	Data T
	// RequestID is the server-issued correlation identifier. Surface
	// in logs and support tickets.
	RequestID string
	// IdempotencyKey is the value sent in the Idempotency-Key header.
	// Set on every request, mutating or otherwise, so the consumer can
	// correlate the SDK-minted value with a downstream replay system.
	IdempotencyKey string
	// Livemode reflects the server-side environment indicator (true
	// when the token resolved to a production scope).
	Livemode bool
	// RetryAttempts is the number of additional attempts the SDK made
	// after the first. Zero means "succeeded on first try". Non-zero
	// values are diagnostic gold for audit traces.
	RetryAttempts int
}

// RawResponse exposes the underlying HTTP response metadata after the
// body has already been consumed and parsed. The body itself is NOT
// re-exposed — the typed Data slot is the source of truth.
type RawResponse struct {
	// Status is the integer HTTP status code (e.g. 200, 422).
	Status int
	// Header carries every response header as received. Header values
	// are NOT redacted here — the consumer asked for raw access.
	Header http.Header
	// URL is the absolute URL the request was sent to.
	URL *url.URL
}

// captureRawResponse builds a RawResponse from a real http.Response.
// Kept private — operations construct it inline so the surface area
// stays minimal.
func captureRawResponse(resp *http.Response) *RawResponse {
	if resp == nil {
		return nil
	}
	out := &RawResponse{
		Status: resp.StatusCode,
		Header: resp.Header.Clone(),
	}
	if resp.Request != nil {
		out.URL = resp.Request.URL
	}
	return out
}
