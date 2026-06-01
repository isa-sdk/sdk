// Package zyins — branding sub-service (GET /v1/branding).
//
// Branding is per-license-order whitelabel configuration: agency name,
// logo URL, colors, and product restrictions. Identity is derived from
// the auth context — the request carries no body credentials. The
// server deliberately does NOT 404 when a row is missing; it returns
// a zero-value BrandingDetail.
//
// See docs/design/cases-email-branding-surface.md for the #149 auth
// elevation context; this SDK surface is unaffected by the eventual
// migration from License-HMAC to session credentials.

package zyins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const brandingLookupPath = "/v1/branding"

// BrandingService groups branding lookup. Mutating operations
// (Upsert / RestrictionsUpsert) ship with the #149 cutover.
type BrandingService struct {
	client *Client
}

// BrandingDetail is the whitelabel configuration returned by
// BrandingService.Lookup. Zero values are returned when no row exists
// for the caller's license (the server intentionally does not 404).
type BrandingDetail struct {
	IMOName                 string `json:"imo_name"`
	IMOLogo                 string `json:"imo_logo"`
	NavColor                string `json:"nav_color"`
	MainColor               string `json:"main_color"`
	ButtonColor             string `json:"button_color"`
	ActiveButtonColor       string `json:"active_button_color"`
	BGColor                 string `json:"bg_color"`
	HeaderTextColor         string `json:"header_text_color"`
	HideAffiliateLeads      bool   `json:"hide_affiliate_leads"`
	PreventProductSelection bool   `json:"prevent_product_selection"`
	DefaultSettings         string `json:"default_settings"`
}

// brandingWireDetail accepts both the bool and string-typed renderings
// of the affiliate / product-selection flags the legacy handler returns
// (boolean today, "true"/"false" historically). The normalizing copy
// into BrandingDetail keeps the public API single-typed.
type brandingWireDetail struct {
	IMOName                 string          `json:"imo_name"`
	IMOLogo                 string          `json:"imo_logo"`
	NavColor                string          `json:"nav_color"`
	MainColor               string          `json:"main_color"`
	ButtonColor             string          `json:"button_color"`
	ActiveButtonColor       string          `json:"active_button_color"`
	BGColor                 string          `json:"bg_color"`
	HeaderTextColor         string          `json:"header_text_color"`
	HideAffiliateLeads      json.RawMessage `json:"hide_affiliate_leads"`
	PreventProductSelection json.RawMessage `json:"prevent_product_selection"`
	DefaultSettings         string          `json:"default_settings"`
}

// Lookup fetches the whitelabel branding for the caller's license.
func (s *BrandingService) Lookup(ctx context.Context, opts ...RunOption) (*BrandingDetail, error) {
	_ = collectRunOptions(opts) // reserved for future per-call options
	raw, err := s.client.doJSON(ctx, requestArgs{
		method: http.MethodGet,
		path:   brandingLookupPath,
		op:     "branding_lookup",
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Branding.Lookup: %w", err)
	}
	return decodeBrandingResponse(raw)
}

func decodeBrandingResponse(body []byte) (*BrandingDetail, error) {
	data, err := unwrapEnvelope(body, "branding_lookup")
	if err != nil {
		return nil, err
	}
	var wire brandingWireDetail
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode branding response: %w", err)
	}
	return &BrandingDetail{
		IMOName:                 wire.IMOName,
		IMOLogo:                 wire.IMOLogo,
		NavColor:                wire.NavColor,
		MainColor:               wire.MainColor,
		ButtonColor:             wire.ButtonColor,
		ActiveButtonColor:       wire.ActiveButtonColor,
		BGColor:                 wire.BGColor,
		HeaderTextColor:         wire.HeaderTextColor,
		HideAffiliateLeads:      coerceWireBool(wire.HideAffiliateLeads),
		PreventProductSelection: coerceWireBool(wire.PreventProductSelection),
		DefaultSettings:         wire.DefaultSettings,
	}, nil
}

// coerceWireBool accepts native JSON booleans, the strings "true" /
// "1", or empty/missing values. The branding handler has shipped both
// representations over time; the SDK normalizes to a Go bool.
func coerceWireBool(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s == "true" || s == "1"
	}
	return false
}
