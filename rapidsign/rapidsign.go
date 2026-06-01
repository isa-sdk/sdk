package rapidsign

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	coretransport "github.com/isa-sdk/sdk/core/transport"
	"github.com/isa-sdk/sdk/rapidsign/internal"
)

// DefaultBaseURL is the production RapidSign endpoint. Override via
// Options.BaseURL for staging, sandbox, or local-dev pointing at a
// fixture server.
const DefaultBaseURL = "https://rapidsign.isaapi.com"

// Default polling cadence for Documents.AwaitSignature. Jittered
// exponential backoff starting at PollInitialInterval, doubling each
// iteration up to PollMaxInterval.
const (
	pollInitialInterval   = 2 * time.Second
	pollMaxInterval       = 30 * time.Second
	pollJitterNumerator   = 1
	pollJitterDenominator = 4
)

// Options configures a Client at construction. All fields are
// optional; New supplies safe defaults for omitted values.
type Options struct {
	// BaseURL overrides DefaultBaseURL. Trailing slash is trimmed.
	BaseURL string
	// HTTPClient overrides the default http.Client. The provided client
	// is wrapped — its existing transport is preserved, bearer and
	// retry transports are layered on top.
	HTTPClient *http.Client
	// UserAgent is appended to outbound requests as the User-Agent
	// header. Empty defaults to a versioned SDK string.
	UserAgent string
	// MaxAttempts caps total request attempts including the first.
	// Zero falls back to coretransport.DefaultMaxAttempts.
	MaxAttempts int

	// clock, ids, and sleeper are test seams. Production callers leave
	// them nil and the constructor wires the real implementations.
	clock   internal.Clock
	ids     *internal.IDSource
	sleeper internal.Sleeper
}

// Client is the top-level RapidSign API client. Construct one with
// New and reuse it across requests; it is safe for concurrent use.
type Client struct {
	baseURL   string
	userAgent string
	doer      internal.Doer
	clock     internal.Clock
	ids       *internal.IDSource
	sleeper   internal.Sleeper

	// Documents groups the document-lifecycle operations. Always non-
	// nil on a Client returned from New.
	Documents *Documents
	// Webhooks groups webhook-verification helpers. Always non-nil on a
	// Client returned from New.
	Webhooks *Webhooks
}

// New returns a Client authenticated with the supplied bearer token.
// The token is read once at construction; callers needing per-request
// token rotation should construct a fresh Client (cheap) or pass an
// http.Client that already injects credentials.
//
// New never panics. Misconfiguration (empty token, invalid base URL)
// is reported as a returned error so callers can decide how to react.
func New(token string, opts ...Options) (*Client, error) {
	if len(opts) > 1 {
		return nil, errors.New("rapidsign: New accepts at most one Options value")
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if strings.TrimSpace(token) == "" && o.HTTPClient == nil {
		return nil, errors.New("rapidsign: New requires a non-empty bearer token or a custom HTTPClient")
	}

	baseURL, err := normalizeBaseURL(o.BaseURL)
	if err != nil {
		return nil, err
	}

	httpClient := o.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	// Inner doer = the supplied http.Client. Layer retry, then bearer.
	// Order matters: bearer runs first on each attempt so retried
	// requests carry a freshly-resolved token.
	retry, err := coretransport.NewRetryTransport(httpClient, coretransport.RetryConfig{
		MaxAttempts: o.MaxAttempts,
	})
	if err != nil {
		return nil, err
	}
	var doer internal.Doer = retry
	if strings.TrimSpace(token) != "" {
		bearer, err := coretransport.NewBearerTransport(coretransport.StaticToken(token), retry)
		if err != nil {
			return nil, err
		}
		doer = bearer
	}

	clock := o.clock
	if clock == nil {
		clock = internal.RealClock()
	}
	ids := o.ids
	if ids == nil {
		ids = internal.RealIDSource()
	}
	sleeper := o.sleeper
	if sleeper == nil {
		sleeper = internal.RealSleeper{}
	}

	c := &Client{
		baseURL:   baseURL,
		userAgent: defaultUserAgent(o.UserAgent),
		doer:      doer,
		clock:     clock,
		ids:       ids,
		sleeper:   sleeper,
	}
	c.Documents = &Documents{c: c}
	c.Webhooks = &Webhooks{c: c}
	return c, nil
}

// normalizeBaseURL trims whitespace, validates scheme/host, and removes a
// trailing slash. An empty or whitespace-only value selects DefaultBaseURL.
func normalizeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultBaseURL, nil
	}
	withoutSlash := strings.TrimRight(trimmed, "/")
	parsed, err := url.Parse(withoutSlash)
	if err != nil {
		return "", fmt.Errorf("rapidsign: invalid Options.BaseURL %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("rapidsign: invalid Options.BaseURL %q: scheme must be http or https", raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("rapidsign: invalid Options.BaseURL %q: host is required", raw)
	}
	return withoutSlash, nil
}

// defaultUserAgent returns the supplied UA or a versioned SDK string.
func defaultUserAgent(supplied string) string {
	if supplied != "" {
		return supplied
	}
	return "isa-sdk-go-rapidsign/0.1"
}
