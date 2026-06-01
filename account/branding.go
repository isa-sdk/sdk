package account

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const brandingPath = "/v1/branding"

// BrandingDetail mirrors the TS BrandingDetail. The server returns a
// zero-value document when no branding row exists (it does NOT 404), so
// the SDK never synthesizes a "no branding" error.
type BrandingDetail struct {
	IMOName                 string `json:"imo_name"`
	IMOLogo                 string `json:"imo_logo"`
	PrimaryColor            string `json:"primary_color"`
	NavColor                string `json:"nav_color"`
	BGColor                 string `json:"bg_color"`
	ButtonColor             string `json:"button_color"`
	ActiveButtonColor       string `json:"active_button_color"`
	HeaderTextColor         string `json:"header_text_color"`
	HideAffiliateLeads      bool   `json:"hide_affiliate_leads"`
	PreventProductSelection bool   `json:"prevent_product_selection"`
	DefaultSettings         string `json:"default_settings"`
}

// BrandingLookupOptions captures optional inputs for Branding.Lookup.
// Source is reserved for the future per-vendor branding endpoint.
type BrandingLookupOptions struct {
	Source string
}

// BrandingService is the `account.branding` facade. Reach it via
// Client.Branding; do not construct directly.
type BrandingService struct {
	client *Client
}

// Lookup fetches the whitelabel branding for the caller's license.
// A nil opts is treated as the zero value.
func (s *BrandingService) Lookup(ctx context.Context, opts *BrandingLookupOptions) (*BrandingDetail, error) {
	path := brandingPath
	if opts != nil && opts.Source != "" {
		path = brandingPath + "?source=" + url.QueryEscape(opts.Source)
	}
	body, err := s.client.signedDo(ctx, callArgs{method: http.MethodGet, path: path})
	if err != nil {
		return nil, fmt.Errorf("account: Branding.Lookup: %w", err)
	}
	return parseBranding(body)
}

func parseBranding(body []byte) (*BrandingDetail, error) {
	out := &BrandingDetail{}
	if len(body) == 0 {
		return out, nil
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, fmt.Errorf("account: Branding parse envelope: %w", err)
	}
	if len(data) == 0 {
		return out, nil
	}
	// Try the strict shape first.
	var primary brandingWire
	if err := json.Unmarshal(data, &primary); err != nil {
		return nil, fmt.Errorf("account: Branding decode: %w", err)
	}
	out.IMOName = primary.IMOName
	out.IMOLogo = primary.IMOLogo
	out.PrimaryColor = primary.PrimaryColor
	if out.PrimaryColor == "" {
		out.PrimaryColor = primary.MainColor
	}
	out.NavColor = primary.NavColor
	out.BGColor = primary.BGColor
	out.ButtonColor = primary.ButtonColor
	out.ActiveButtonColor = primary.ActiveButtonColor
	out.HeaderTextColor = primary.HeaderTextColor
	out.HideAffiliateLeads = primary.HideAffiliateLeads
	out.PreventProductSelection = primary.PreventProductSelection
	out.DefaultSettings = primary.DefaultSettings
	return out, nil
}

// brandingWire is the on-wire JSON shape; we accept both `primary_color`
// (canonical) and `main_color` (legacy) so older servers keep working.
type brandingWire struct {
	IMOName                 string `json:"imo_name"`
	IMOLogo                 string `json:"imo_logo"`
	PrimaryColor            string `json:"primary_color"`
	MainColor               string `json:"main_color"`
	NavColor                string `json:"nav_color"`
	BGColor                 string `json:"bg_color"`
	ButtonColor             string `json:"button_color"`
	ActiveButtonColor       string `json:"active_button_color"`
	HeaderTextColor         string `json:"header_text_color"`
	HideAffiliateLeads      bool   `json:"hide_affiliate_leads"`
	PreventProductSelection bool   `json:"prevent_product_selection"`
	DefaultSettings         string `json:"default_settings"`
}
