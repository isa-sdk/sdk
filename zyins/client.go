package zyins

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	coretransport "github.com/isa-sdk/sdk/core/transport"
	"github.com/isa-sdk/sdk/zyins/cases"
	"github.com/isa-sdk/sdk/zyins/reference"
)

// DefaultBaseURL is the production ZyINS endpoint. Override with
// WithBaseURL for staging, sandbox, or local-development fixture
// servers.
const DefaultBaseURL = "https://zyins.isaapi.com"

// defaultHTTPTimeout is the per-request ceiling applied when the
// caller does not supply a custom *http.Client.
const defaultHTTPTimeout = 60 * time.Second

// Client is the top-level ZyINS API client. Construct one with
// NewClient and reuse it across requests; it is safe for concurrent
// use (see README "Concurrency"). Operations are grouped under typed
// sub-services as exported fields.
type Client struct {
	baseURL   string
	userAgent string
	doer      httpDoer
	// bootstrapDoer skips the BearerTransport wrap so credential-bootstrap
	// endpoints (e.g., /v2/licenses/{activate,check,deactivate}) do not
	// send an Authorization header. These three operations sit OUTSIDE the
	// server's AuthMiddleware: activate is what mints the license key, so
	// the client cannot sign with a credential it does not yet have.
	bootstrapDoer httpDoer
	logger        DebugLogger
	// apiVersionOverrides captures the per-instance per-surface
	// version pins supplied via WithAPIVersionOverrides. nil means
	// "use BundledAPIVersions verbatim".
	apiVersionOverrides map[string]string
	// caseStorage backs the CasesService.Save / CasesService.Delete
	// wrappers. When the caller supplied one via WithCaseStorage it is
	// set at construction; otherwise it stays nil and the zero-knowledge
	// default is constructed exactly once via caseStorageOnce.
	caseStorage cases.CaseStorage
	// caseStorageOnce guards the lazy construction of the zero-knowledge
	// default so concurrent Save/Recall calls do not race on caseStorage.
	caseStorageOnce sync.Once
	// refIndex memoizes the cache-backed *reference.Index used by the
	// top-level Medications/Conditions/Concepts matchers. See
	// match_top.go.
	refIndex referenceIndexCache

	// adapters holds the reference-namespace adapter overrides and
	// memoized defaults. See autocorrector_facade.go.
	adapters referenceAdapters

	// Prequalify runs the prequalify engine against an applicant.
	Prequalify *PrequalifyService
	// Quote computes a premium for one accepted plan after prequalify.
	Quote *QuoteService
	// Datasets reads the read-only datasets surface (conditions,
	// medications, brands, plans).
	Datasets *DatasetsService
	// Products provides a memoized product catalog fetched from the
	// server once and cached for subsequent calls.
	Products *ProductsService
	// ReferenceData reads engine reference tables (states, products,
	// nicotine modes).
	ReferenceData *ReferenceDataService
	// Usage reads consumption and quota counters.
	Usage *UsageService
	// License runs public license-lifecycle calls (Check, Deactivate).
	// Each device has exactly one license; this singular field is the
	// canonical SDK surface per the locked SDK syntax (TS canon:
	// isa.zyins.license). The authenticated self-* surface lands with
	// the LicenseHMAC transport in a follow-up PR.
	License *LicenseService
	// Health exposes the platform readiness probe (/ready). Liveness
	// (/health) lands in a follow-up PR.
	Health *HealthService
	// Branding fetches the whitelabel branding for the caller's
	// license. See branding.go.
	Branding *BrandingService
	// Preferences fetches and upserts the opaque per-license
	// preferences document. See preferences.go.
	Preferences *PreferencesService
	// Cases creates shareable cases and shares them by email. See
	// cases.go.
	Cases *CasesService
	// Email enqueues transactional emails. See cases.go.
	Email *EmailService
	// Logos serves the public carrier-logo asset endpoint. Non-credentialed
	// per api-standards.md (GET allowlist); no bearer/HMAC headers attached.
	Logos *LogosService

	// PrequalifyV3 runs the v3 prequalify engine with the uniform
	// pricing[] table shape. See packages/ts/src/zyins/prequalify-v3.ts
	// for the binding reference; the wire contract is in ADR-035.
	PrequalifyV3 *PrequalifyV3Service
	// QuoteV3 runs the v3 quote engine, grouping qualifying products
	// by requested amount.
	QuoteV3 *QuoteV3Service
	// DatasetsV3 reads the typed id-keyed reference catalog at
	// GET /v3/datasets. Use with Reference to resolve free text into
	// typed Concept handles.
	DatasetsV3 *DatasetsV3Service
	// Reference resolves free text into typed Concept handles
	// (medications, conditions, unknown). Stateless; consumers pass a
	// *DatasetBundleV3 explicitly to every Match call.
	Reference *ReferenceService
}

// Option mutates an *options block during NewClient. The functional-
// options pattern keeps the public constructor signature compact while
// allowing additive evolution (new options never break call sites).
type Option func(*options) error

// options carries the resolved construction-time configuration.
type options struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource TokenSource
	userAgent   string
	timeout     time.Duration
	maxAttempts int
	// license and session capture credentials from the License/Session
	// factories. The transports for these modes ship after the
	// bearer-only baseline; NewClient rejects them with a typed
	// *ConfigError until then so callers see the gap at construction.
	license *LicenseCredential
	session *SessionCredential
	// logger overrides the default stderr debug logger. nil means
	// "use the slog handler honoring ISA_LOG".
	logger DebugLogger
	// apiVersionOverrides supplies per-surface version pins layered on
	// top of [BundledAPIVersions].
	apiVersionOverrides map[string]string
	// caseStorage swaps the default zero-knowledge case store. nil
	// resolves to [cases.NewZeroKnowledgeCaseStorage] at first use.
	caseStorage cases.CaseStorage
	// autocorrector overrides the default reference autocorrector.
	autocorrector reference.Autocorrector
	// matchAlgorithm overrides the default reference match algorithm.
	matchAlgorithm reference.MatchAlgorithm
	// autocompleteAlgorithm overrides the default reference
	// autocomplete algorithm.
	autocompleteAlgorithm reference.AutocompleteAlgorithm
}

// WithToken supplies a static bearer token. Equivalent to
// WithTokenSource(StaticToken(token)). Token shape is validated; the
// token must start with `isa_live_` or `isa_test_`.
func WithToken(token string) Option {
	return func(o *options) error {
		if err := validateTokenShape(token); err != nil {
			return fmt.Errorf("zyins: WithToken rejected token: %w", err)
		}
		o.tokenSource = StaticToken(token)
		return nil
	}
}

// WithTokenSource supplies a refreshing TokenSource. Callers that
// rotate credentials in-process pass an implementation here; the SDK
// invokes Token() once per outbound request.
func WithTokenSource(src TokenSource) Option {
	return func(o *options) error {
		if src == nil {
			return errors.New("zyins: WithTokenSource requires a non-nil TokenSource")
		}
		o.tokenSource = src
		return nil
	}
}

// WithBaseURL overrides DefaultBaseURL. Use for staging, sandbox, or
// fixture servers. Trailing slashes are trimmed.
func WithBaseURL(url string) Option {
	return func(o *options) error {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			return errors.New("zyins: WithBaseURL requires a non-empty URL")
		}
		o.baseURL = strings.TrimRight(trimmed, "/")
		return nil
	}
}

// WithHTTPClient overrides the SDK's default *http.Client. The supplied
// client's transport is preserved; the bearer + retry transports layer
// on top.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) error {
		if c == nil {
			return errors.New("zyins: WithHTTPClient requires a non-nil *http.Client")
		}
		o.httpClient = c
		return nil
	}
}

// WithTimeout sets the per-request HTTP timeout. Ignored when the
// caller also supplies WithHTTPClient — that client's timeout wins.
func WithTimeout(d time.Duration) Option {
	return func(o *options) error {
		if d <= 0 {
			return errors.New("zyins: WithTimeout requires a positive duration")
		}
		o.timeout = d
		return nil
	}
}

// WithUserAgent overrides the default User-Agent header. Useful for
// callers that want their app name surfaced in the server logs.
func WithUserAgent(ua string) Option {
	return func(o *options) error {
		if strings.TrimSpace(ua) == "" {
			return errors.New("zyins: WithUserAgent requires a non-empty value")
		}
		o.userAgent = ua
		return nil
	}
}

// WithLogger overrides the default stderr debug logger. Any
// implementation of DebugLogger is accepted (slog, custom adapter,
// etc.). Passing nil resets to the default.
func WithLogger(l DebugLogger) Option {
	return func(o *options) error {
		o.logger = l
		return nil
	}
}

// WithMaxRetryAttempts caps the total request attempts including the
// first. The value MUST be a positive integer; zero and negative
// counts are rejected as programming errors because "no retries" is
// not the same intent as "use the SDK default" — callers who want the
// default should omit this option entirely.
func WithMaxRetryAttempts(n int) Option {
	return func(o *options) error {
		if n <= 0 {
			return errors.New("zyins: WithMaxRetryAttempts requires a positive count")
		}
		o.maxAttempts = n
		return nil
	}
}

// WithAPIVersionOverrides supplies per-surface version pins layered
// on top of [BundledAPIVersions]. Keys are surface names ("prequalify",
// "quote", ...); values are version prefixes ("v1", "v2", ...). The
// SDK reads the resolved value via [Client.APIVersionFor] when
// building per-surface request paths.
//
// Passing nil resets to the bundled defaults. Empty-string values are
// rejected — a surface that should fall through to the bundled value
// must be omitted from the map entirely.
func WithAPIVersionOverrides(overrides map[string]string) Option {
	return func(o *options) error {
		if overrides == nil {
			o.apiVersionOverrides = nil
			return nil
		}
		copied := make(map[string]string, len(overrides))
		for surface, version := range overrides {
			if surface == "" {
				return errors.New("zyins: WithAPIVersionOverrides surface key must be non-empty")
			}
			if version == "" {
				return fmt.Errorf("zyins: WithAPIVersionOverrides[%q] must be non-empty", surface)
			}
			copied[surface] = version
		}
		o.apiVersionOverrides = copied
		return nil
	}
}

// WithCaseStorage swaps the default zero-knowledge case store. Passing
// nil resets to the default ([cases.NewZeroKnowledgeCaseStorage]
// wrapping the underlying client).
func WithCaseStorage(storage cases.CaseStorage) Option {
	return func(o *options) error {
		o.caseStorage = storage
		return nil
	}
}

// WithAutocorrector installs a caller-supplied
// [reference.Autocorrector] as the value returned by
// [Client.Autocorrector]. Passing nil clears any prior override and
// falls back to the default (built from the v3 spelling_corrections
// dataset on first use).
func WithAutocorrector(a reference.Autocorrector) Option {
	return func(o *options) error {
		o.autocorrector = a
		return nil
	}
}

// WithMatchAlgorithm installs a caller-supplied
// [reference.MatchAlgorithm]. Passing nil clears the override; the
// default is the key-equality algorithm.
func WithMatchAlgorithm(m reference.MatchAlgorithm) Option {
	return func(o *options) error {
		o.matchAlgorithm = m
		return nil
	}
}

// WithAutocompleteAlgorithm installs a caller-supplied
// [reference.AutocompleteAlgorithm]. Passing nil clears the override;
// the default is the bucketed algorithm.
func WithAutocompleteAlgorithm(a reference.AutocompleteAlgorithm) Option {
	return func(o *options) error {
		o.autocompleteAlgorithm = a
		return nil
	}
}

// NewClient returns a ready-to-use Client. The single required option
// is WithToken or WithTokenSource; everything else falls back to the
// SDK defaults.
//
// NewClient never panics: every misconfiguration is surfaced as a
// returned error.
func NewClient(opts ...Option) (*Client, error) {
	o := options{
		baseURL:   DefaultBaseURL,
		userAgent: userAgentHeader,
		timeout:   defaultHTTPTimeout,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&o); err != nil {
			return nil, fmt.Errorf("zyins: NewClient option failed: %w", err)
		}
	}
	if o.tokenSource == nil {
		// Surface pending-transport credentials with a typed error so
		// callers can distinguish "you forgot to configure auth" from
		// "License/Session auth is captured but not yet wired".
		if o.license != nil {
			return nil, errLicenseTransportPending
		}
		if o.session != nil {
			return nil, errSessionTransportPending
		}
		return nil, errors.New("zyins: NewClient requires WithToken, WithTokenSource, or WithBearer")
	}

	httpClient := o.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: o.timeout}
	}

	retry, err := coretransport.NewRetryTransport(httpClient, coretransport.RetryConfig{
		MaxAttempts: o.maxAttempts,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: NewClient retry transport: %w", err)
	}
	logger := o.logger
	if logger == nil {
		logger = newDefaultDebugLogger()
	}
	// Wire order: bearer wraps debug wraps retry. Putting debug INSIDE
	// bearer means the Authorization header is already attached at log
	// time so redaction has something to redact; putting debug OUTSIDE
	// retry keeps a single log entry per logical request rather than
	// one per retry attempt.
	debugged := &debugDoer{inner: retry, logger: logger}
	bearer, err := coretransport.NewBearerTransport(asCoreTokenSource(o.tokenSource), debugged)
	if err != nil {
		return nil, fmt.Errorf("zyins: NewClient bearer transport: %w", err)
	}
	c := &Client{
		baseURL:             o.baseURL,
		userAgent:           o.userAgent,
		doer:                bearer,
		bootstrapDoer:       debugged,
		logger:              logger,
		apiVersionOverrides: o.apiVersionOverrides,
		caseStorage:         o.caseStorage,
		adapters: referenceAdapters{
			autocorrectorOverride:         o.autocorrector,
			matchAlgorithmOverride:        o.matchAlgorithm,
			autocompleteAlgorithmOverride: o.autocompleteAlgorithm,
		},
	}
	c.Prequalify = &PrequalifyService{client: c}
	c.Quote = &QuoteService{client: c}
	c.Datasets = &DatasetsService{client: c}
	c.Products = &ProductsService{client: c}
	c.ReferenceData = &ReferenceDataService{client: c}
	c.Usage = &UsageService{client: c}
	c.License = &LicenseService{client: c}
	c.Health = &HealthService{client: c}
	c.Branding = &BrandingService{client: c}
	c.Preferences = &PreferencesService{client: c}
	c.Cases = &CasesService{client: c}
	c.Email = &EmailService{client: c}
	c.Logos = &LogosService{client: c}
	c.PrequalifyV3 = &PrequalifyV3Service{client: c}
	c.QuoteV3 = &QuoteV3Service{client: c}
	c.DatasetsV3 = &DatasetsV3Service{client: c}
	c.Reference = newReferenceService()
	return c, nil
}

// tokenSourceAdapter bridges the SDK's TokenSource interface to the
// core package's identically-shaped interface. Two declarations rather
// than one re-export so the public API of this package does not depend
// on consumers importing sdk/core just to satisfy a type assertion.
type tokenSourceAdapter struct{ inner TokenSource }

func (t tokenSourceAdapter) Token() (string, error) {
	tok, err := t.inner.Token()
	if err != nil {
		return "", fmt.Errorf("zyins: token source returned error: %w", err)
	}
	return tok, nil
}

func asCoreTokenSource(src TokenSource) coretransport.TokenSource {
	return tokenSourceAdapter{inner: src}
}
