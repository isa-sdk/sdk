// Package zyins — preferences sub-service (GET/POST /v1/preferences).
//
// Preferences are an opaque JSON document stored per (email, license_order).
// The SDK does not interpret the document; callers serialize their own
// settings shape and pass through. Identity is derived from the auth
// context — body carries no credentials.

package zyins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const preferencesPath = "/v1/preferences"

// PreferencesService groups preferences lookup + upsert.
type PreferencesService struct {
	client *Client
}

// PreferencesDocument is the opaque preferences payload. The SDK
// preserves caller-supplied keys / values verbatim.
type PreferencesDocument map[string]any

// PreferencesResult is the typed response from PreferencesService.Lookup
// and PreferencesService.Set.
type PreferencesResult struct {
	Prefs PreferencesDocument `json:"prefs"`
}

// PreferencesSetInput is the input shape for PreferencesService.Set.
type PreferencesSetInput struct {
	// Prefs is the document to upsert. Required.
	Prefs PreferencesDocument
}

// prefsWireBody mirrors the on-wire request shape.
type prefsWireBody struct {
	Prefs PreferencesDocument `json:"prefs"`
}

// Lookup fetches the caller's preferences document.
func (s *PreferencesService) Lookup(ctx context.Context, opts ...RunOption) (*PreferencesResult, error) {
	_ = collectRunOptions(opts)
	raw, err := s.client.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   preferencesPath,
		op:     "preferences_lookup",
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Preferences.Lookup: %w", err)
	}
	return decodePreferencesResponse(raw, nil)
}

// Set upserts the caller's preferences document.
func (s *PreferencesService) Set(ctx context.Context, input *PreferencesSetInput, opts ...RunOption) (*PreferencesResult, error) {
	if input == nil {
		return nil, validationFailure("zyins: PreferencesSetInput is nil")
	}
	if input.Prefs == nil {
		return nil, validationFailure("zyins: PreferencesSetInput.Prefs is required")
	}
	ro := collectRunOptions(opts)
	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           preferencesPath,
		body:           prefsWireBody{Prefs: input.Prefs},
		op:             "preferences_set",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Preferences.Set: %w", err)
	}
	return decodePreferencesResponse(raw, input.Prefs)
}

// decodePreferencesResponse tolerates: an enveloped { data: { prefs: {...} } },
// a bare { prefs: {...} }, an empty body (falls back to the request prefs),
// or a bare object treated as the prefs document.
func decodePreferencesResponse(body []byte, fallback PreferencesDocument) (*PreferencesResult, error) {
	if len(body) == 0 {
		return &PreferencesResult{Prefs: ensureNonNilPrefs(fallback)}, nil
	}
	data, err := unwrapEnvelope(body, "preferences")
	if err != nil {
		return nil, err
	}
	var withPrefs struct {
		Prefs PreferencesDocument `json:"prefs"`
	}
	if err := json.Unmarshal(data, &withPrefs); err == nil && withPrefs.Prefs != nil {
		return &PreferencesResult{Prefs: withPrefs.Prefs}, nil
	}
	var bare PreferencesDocument
	if err := json.Unmarshal(data, &bare); err == nil && bare != nil {
		return &PreferencesResult{Prefs: bare}, nil
	}
	return &PreferencesResult{Prefs: ensureNonNilPrefs(fallback)}, nil
}

func ensureNonNilPrefs(d PreferencesDocument) PreferencesDocument {
	if d == nil {
		return PreferencesDocument{}
	}
	return d
}
