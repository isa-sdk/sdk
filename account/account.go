// Package account is the License-HMAC-authenticated `isa.account.*`
// surface — branding, preferences, cases, email, and reference-data
// lookups for a single ZyINS license.
//
// The package mirrors the TypeScript SDK's `account/` module (PR #196):
// every method is a thin wrapper around an HTTP call signed with the
// License-HMAC header bundle. Construction is lazy and cheap; a Client
// holds one Auth context, one Transport, and one Clock, then exposes
// sub-services via fields.
//
// Construction example:
//
//	c, err := account.NewClient(account.Auth{
//	    LicenseKey: lic,
//	    OrderID:    keycode,
//	    Email:      email,
//	    DeviceID:   deviceID,
//	}, account.WithBaseURL(stagingURL))
//	if err != nil { return err }
//	branding, err := c.Branding.Lookup(ctx, nil)
//
// The package intentionally lives next to (not inside) the bearer-only
// `zyins` client because the License-HMAC transport has no overlap with
// the bearer transport.
package account

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/isa-sdk/sdk/core/license"
)

// DefaultBaseURL is the production ZyINS endpoint. Override via
// WithBaseURL when targeting staging / sandbox / fixtures.
const DefaultBaseURL = "https://zyins.isaapi.com"

const defaultHTTPTimeout = 60 * time.Second

// Auth carries the license credentials shared by every account.* call.
// Fields are required; NewClient rejects construction with any of them
// empty so each call site does not re-validate.
type Auth struct {
	LicenseKey string
	OrderID    string
	Email      string
	DeviceID   string
}

// HTTPDoer is the minimal HTTP contract the account client depends on.
// `*http.Client` satisfies it.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Options is the construction-time configuration for Client.
type Options struct {
	Auth       Auth
	BaseURL    string
	HTTPClient HTTPDoer
	// Clock is the timestamp source for HMAC headers; nil → time.Now.
	Clock license.Clock
}

// Option mutates an Options block during NewClient. Functional options
// keep the constructor signature compact while letting additive surface
// land without churn at call sites.
type Option func(*Options)

// WithBaseURL overrides DefaultBaseURL.
func WithBaseURL(url string) Option { return func(o *Options) { o.BaseURL = url } }

// WithHTTPClient overrides the SDK's default *http.Client.
func WithHTTPClient(c HTTPDoer) Option { return func(o *Options) { o.HTTPClient = c } }

// WithClock overrides the HMAC timestamp clock. Tests inject a fixed
// clock to make signatures reproducible.
func WithClock(c license.Clock) Option { return func(o *Options) { o.Clock = c } }

// Client is the top-level `isa.account.*` namespace. Construct one per
// process; it is safe for concurrent use.
type Client struct {
	auth       Auth
	baseURL    string
	httpClient HTTPDoer
	clock      license.Clock

	// Branding fetches whitelabel configuration for the calling license.
	Branding *BrandingService
	// Preferences reads and writes the per-scope opaque settings document.
	Preferences *PreferencesService
	// Cases creates, retrieves, lists, and shares quote cases.
	Cases *CasesService
	// Email enqueues transactional email.
	Email *EmailService
	// ReferenceData reads engine reference tables.
	ReferenceData *ReferenceDataService
}

// NewClient constructs the account namespace. Required field is Auth;
// empty subfields trigger an immediate construction error so callers
// see misconfiguration at startup rather than at the first call.
func NewClient(auth Auth, opts ...Option) (*Client, error) {
	if strings.TrimSpace(auth.LicenseKey) == "" ||
		strings.TrimSpace(auth.OrderID) == "" ||
		strings.TrimSpace(auth.Email) == "" ||
		strings.TrimSpace(auth.DeviceID) == "" {
		return nil, errors.New("account: NewClient requires non-empty LicenseKey, OrderID, Email, DeviceID")
	}
	o := Options{Auth: auth, BaseURL: DefaultBaseURL}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	c := &Client{
		auth:       o.Auth,
		baseURL:    o.BaseURL,
		httpClient: o.HTTPClient,
		clock:      o.Clock,
	}
	c.Branding = &BrandingService{client: c}
	c.Preferences = &PreferencesService{client: c}
	c.Cases = &CasesService{client: c}
	c.Email = &EmailService{client: c}
	c.ReferenceData = &ReferenceDataService{client: c}
	return c, nil
}
