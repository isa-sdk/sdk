package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const preferencesPath = "/v1/preferences"

// PreferencesDocument is the opaque settings document. Keys and values
// are caller-defined; the SDK does not interpret them.
type PreferencesDocument map[string]any

// PreferencesService is the `account.preferences` facade.
type PreferencesService struct {
	client *Client
}

// Lookup fetches the preferences document for the supplied scope. Scope
// is required; the surface partitions per-license settings so different
// products do not stomp each other.
func (s *PreferencesService) Lookup(ctx context.Context, scope string) (PreferencesDocument, error) {
	if scope == "" {
		return nil, errors.New("account: Preferences.Lookup requires a non-empty scope")
	}
	path := preferencesPath + "?scope=" + url.QueryEscape(scope)
	body, err := s.client.signedDo(ctx, callArgs{method: http.MethodGet, path: path})
	if err != nil {
		return nil, fmt.Errorf("account: Preferences.Lookup: %w", err)
	}
	return parsePreferences(body)
}

// Set upserts the preferences document for the supplied scope. The
// Idempotency-Key header is injected by the transport when key is empty;
// callers MAY pass a non-empty key to coordinate retries across processes.
func (s *PreferencesService) Set(ctx context.Context, scope string, prefs PreferencesDocument, opts ...CallOption) (bool, error) {
	if scope == "" {
		return false, errors.New("account: Preferences.Set requires a non-empty scope")
	}
	if prefs == nil {
		return false, errors.New("account: Preferences.Set requires a non-nil prefs document")
	}
	wire := struct {
		Scope string              `json:"scope"`
		Prefs PreferencesDocument `json:"prefs"`
	}{Scope: scope, Prefs: prefs}
	bodyBytes, err := json.Marshal(wire)
	if err != nil {
		return false, fmt.Errorf("account: Preferences.Set marshal: %w", err)
	}
	co := collectCallOptions(opts)
	if _, err := s.client.signedDo(ctx, callArgs{
		method:         http.MethodPost,
		path:           preferencesPath,
		body:           bodyBytes,
		idempotencyKey: co.idempotencyKey,
	}); err != nil {
		return false, fmt.Errorf("account: Preferences.Set: %w", err)
	}
	return true, nil
}

// parsePreferences extracts the document from either `{prefs: {...}}`,
// the standard envelope, or a bare object. Empty body → empty document.
func parsePreferences(body []byte) (PreferencesDocument, error) {
	if len(body) == 0 {
		return PreferencesDocument{}, nil
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, fmt.Errorf("account: Preferences parse envelope: %w", err)
	}
	if len(data) == 0 {
		return PreferencesDocument{}, nil
	}
	var withPrefs struct {
		Prefs PreferencesDocument `json:"prefs"`
	}
	if err := json.Unmarshal(data, &withPrefs); err == nil && withPrefs.Prefs != nil {
		return withPrefs.Prefs, nil
	}
	var flat PreferencesDocument
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil, fmt.Errorf("account: Preferences decode: %w", err)
	}
	if flat == nil {
		flat = PreferencesDocument{}
	}
	return flat, nil
}
