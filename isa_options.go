package sdk

// Typed options-bag constructor for [Isa]. Mirrors
// packages/ts/src/zyins/isaOptions.ts and packages/python/src/sah_sdk/zyins/isa_options.py.
//
// The historic factory functions ([WithBearer], [WithLicense],
// [WithSession]) remain the canonical primitives; [New] is the
// recommended path going forward and matches the cross-language SDK
// shape (TS Isa.create({auth, engine, ...}), Python Isa.create(auth=...,
// engine=...)).
//
//	isa, err := sdk.New(sdk.IsaOptions{
//	    Auth:       sdk.BearerAuth{Token: "isa_live_..."},
//	    Engine:     sdk.RemoteEngine{},
//	    Timeout:    30 * time.Second,
//	    APIVersion: sdk.APIVersionV2,
//	})
//
// APIVersion is immutable per-instance and pinned via the Version
// request header so the server can select the matching contract.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/isa-sdk/sdk/proxy"
	"github.com/isa-sdk/sdk/rapidsign"
	"github.com/isa-sdk/sdk/zyins"
)

// IsaAPIVersion is the pinned API major version per [Isa] instance.
type IsaAPIVersion string

const (
	// APIVersionV1 pins to the legacy /v1/ contract.
	APIVersionV1 IsaAPIVersion = "v1"
	// APIVersionV2 pins to the typed-offer /v2/ contract (default).
	APIVersionV2 IsaAPIVersion = "v2"
)

// DefaultTimeout matches the TS 30_000 ms default.
const DefaultTimeout = 30 * time.Second

// Production endpoint origins.
const (
	ProductionRemoteOrigin = "https://zyins.isaapi.com"
	ProductionProxyOrigin  = "https://proxy.isaapi.com"
)

const apiVersionHeader = "Version"

// AuthSupplier is the auth-supplier discriminated value type accepted
// by [IsaOptions]. Implementations are: [BearerAuth], [LicenseAuth],
// [FormAuth], [SessionAuth]. Each carries the credential material the
// matching factory requires; [New] dispatches by concrete type.
type AuthSupplier interface {
	authKind() string
}

// BearerAuth is the bearer-token auth supplier.
//
// An empty Token resolves from ISA_TOKEN at factory time (matching the
// TS BearerAuth.fromEnv() shape).
type BearerAuth struct {
	Token string
}

func (BearerAuth) authKind() string { return "bearer" }

// NewBearerAuth constructs a [BearerAuth] from an explicit token. Mirrors
// the TS BearerAuth.fromToken(token) factory.
func NewBearerAuth(token string) BearerAuth {
	return BearerAuth{Token: token}
}

// BearerAuthFromEnv constructs a [BearerAuth] that reads ISA_TOKEN at factory
// time. Mirrors the TS BearerAuth.fromEnv() factory.
func BearerAuthFromEnv() BearerAuth {
	return BearerAuth{}
}

// LicenseAuth is the license-credential auth supplier.
//
// Empty Keycode/Email resolve from ISA_LICENSE_KEYCODE /
// ISA_LICENSE_EMAIL at factory time.
type LicenseAuth struct {
	Keycode string
	Email   string
}

func (LicenseAuth) authKind() string { return "license" }

// NewLicenseAuth constructs a [LicenseAuth] from explicit credentials.
func NewLicenseAuth(keycode, email string) LicenseAuth {
	return LicenseAuth{Keycode: keycode, Email: email}
}

// LicenseAuthFromEnv constructs a [LicenseAuth] that reads env vars at factory time.
func LicenseAuthFromEnv() LicenseAuth {
	return LicenseAuth{}
}

// FormAuth is the embedded-form-token auth supplier.
type FormAuth struct {
	FormToken string
}

func (FormAuth) authKind() string { return "form" }

// NewFormAuth constructs a [FormAuth] from a non-empty form token.
func NewFormAuth(formToken string) FormAuth {
	return FormAuth{FormToken: formToken}
}

// SessionAuth is the session-credential auth supplier.
type SessionAuth struct {
	SessionID     string
	SessionSecret string //nolint:gosec // documented credential field
}

func (SessionAuth) authKind() string { return "session" }

// Engine is the engine-selector discriminated value type accepted by
// [IsaOptions]. Implementations are: [RemoteEngine], [LocalEngine],
// [ProxyEngine], [InMemoryEngine].
type Engine interface {
	engineKind() string
	baseURL() string
}

// RemoteEngine routes to the production (or staging) ZyINS endpoint.
//
// An empty BaseURL defaults to [ProductionRemoteOrigin].
type RemoteEngine struct {
	BaseURL string
}

func (RemoteEngine) engineKind() string { return "remote" }
func (e RemoteEngine) baseURL() string {
	if e.BaseURL == "" {
		return ProductionRemoteOrigin
	}
	return e.BaseURL
}

// LocalEngine routes to a developer or test endpoint.
type LocalEngine struct {
	BaseURL string
}

func (LocalEngine) engineKind() string { return "local" }
func (e LocalEngine) baseURL() string {
	return e.BaseURL
}

// ProxyEngine routes through the platform proxy.
//
// The underlying ZyINS request still targets [ProductionRemoteOrigin];
// ProxyOrigin is consumed by the proxy namespace.
type ProxyEngine struct {
	ProxyOrigin string
}

func (ProxyEngine) engineKind() string { return "proxy" }
func (ProxyEngine) baseURL() string    { return ProductionRemoteOrigin }

// InMemoryEngine is reserved for the in-process test engine once the
// Go SDK exposes a transport override on [IsaOptions].
type InMemoryEngine struct{}

func (InMemoryEngine) engineKind() string { return "in_memory" }
func (InMemoryEngine) baseURL() string    { return ProductionRemoteOrigin }

// IsaOptions is the typed options bag accepted by [New]. Every field is
// optional except Auth; defaults match the production posture
// (RemoteEngine{} → production, [DefaultTimeout], [APIVersionV2]).
type IsaOptions struct {
	// Auth is the auth supplier. Required.
	Auth AuthSupplier
	// Engine selects the deployment target. Default: [RemoteEngine]{}.
	Engine Engine
	// Timeout caps each request. Default: [DefaultTimeout].
	Timeout time.Duration
	// APIVersion pins the API major version. Default: [APIVersionV2].
	APIVersion IsaAPIVersion
	// ClientVersion is reserved for the client-version negotiation
	// surface. Non-empty values are rejected until that surface is wired.
	ClientVersion string
}

// ResolvedIsaOptions is the fully-defaulted view of [IsaOptions]
// produced by [ResolveIsaOptions]. Pure value type — safe to pass
// through constructors and tests.
type ResolvedIsaOptions struct {
	Auth          AuthSupplier
	Engine        Engine
	Timeout       time.Duration
	APIVersion    IsaAPIVersion
	ClientVersion string
	BaseURL       string
	ProxyOrigin   string
}

// ResolveIsaOptions applies defaults and returns the resolved view.
// Pure — no side effects, safe to call from constructors and tests.
func ResolveIsaOptions(opts IsaOptions) (ResolvedIsaOptions, error) {
	if opts.Auth == nil {
		return ResolvedIsaOptions{}, errors.New("sdk.New: Auth is required")
	}
	engine := opts.Engine
	if engine == nil {
		engine = RemoteEngine{}
	}
	engine, err := normalizeEngine(engine)
	if err != nil {
		return ResolvedIsaOptions{}, err
	}
	if local, ok := engine.(LocalEngine); ok && local.BaseURL == "" {
		return ResolvedIsaOptions{}, errors.New("sdk.New: LocalEngine requires a non-empty BaseURL")
	}
	if _, ok := engine.(InMemoryEngine); ok {
		return ResolvedIsaOptions{}, errors.New("sdk.New: InMemoryEngine is not wired in the Go SDK yet")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	apiVersion := opts.APIVersion
	if apiVersion == "" {
		apiVersion = APIVersionV2
	}
	if apiVersion != APIVersionV1 && apiVersion != APIVersionV2 {
		return ResolvedIsaOptions{}, fmt.Errorf(
			"sdk.New: APIVersion must be %q or %q (got %q)",
			APIVersionV1, APIVersionV2, apiVersion,
		)
	}
	if opts.ClientVersion != "" {
		return ResolvedIsaOptions{}, errors.New("sdk.New: ClientVersion is not wired in the Go SDK yet")
	}
	var proxyOrigin string
	if pe, ok := engine.(ProxyEngine); ok {
		proxyOrigin = pe.ProxyOrigin
		if proxyOrigin == "" {
			proxyOrigin = ProductionProxyOrigin
		}
	}
	return ResolvedIsaOptions{
		Auth:          opts.Auth,
		Engine:        engine,
		Timeout:       timeout,
		APIVersion:    apiVersion,
		ClientVersion: opts.ClientVersion,
		BaseURL:       engine.baseURL(),
		ProxyOrigin:   proxyOrigin,
	}, nil
}

// New constructs an [Isa] from the typed [IsaOptions] options bag.
// Recommended over [WithBearer] / [WithLicense] / [WithSession] going
// forward; matches the cross-language SDK shape.
//
// New dispatches to the matching legacy factory based on the auth
// supplier concrete type. An empty credential field on the supplier
// (e.g. BearerAuth{} with no Token) defers resolution to the legacy
// factory's env-var fallback.
func New(opts IsaOptions) (*Isa, error) {
	resolved, err := ResolveIsaOptions(opts)
	if err != nil {
		return nil, err
	}
	auth, err := normalizeAuthSupplier(resolved.Auth)
	if err != nil {
		return nil, err
	}
	baseOptions := resolvedZyinsOptions(resolved)
	switch supplier := auth.(type) {
	case BearerAuth:
		token, err := resolveBearerToken(supplier.Token)
		if err != nil {
			return nil, err
		}
		zyinsOptions := append([]zyins.Option{zyins.WithToken(token)}, baseOptions...)
		zc, err := zyins.NewClient(zyinsOptions...)
		if err != nil {
			return nil, fmt.Errorf("sdk.New: zyins.NewClient: %w", err)
		}
		rc, err := rapidsign.New(token, rapidsign.Options{
			HTTPClient: &http.Client{Timeout: resolved.Timeout},
		})
		if err != nil {
			return nil, fmt.Errorf("sdk.New: rapidsign.New: %w", err)
		}
		return newIsa(zc, rc, proxy.SessionBinding{ProxyOrigin: resolvedProxyOrigin(resolved)}), nil
	case LicenseAuth:
		authOption, err := buildLicenseOption(LicenseOptions{
			Keycode: supplier.Keycode,
			Email:   supplier.Email,
		})
		if err != nil {
			return nil, err
		}
		zyinsOptions := append([]zyins.Option{authOption}, baseOptions...)
		zc, err := zyins.NewClient(zyinsOptions...)
		if err != nil {
			return nil, fmt.Errorf("sdk.New: zyins.NewClient: %w", err)
		}
		return newIsa(zc, nil, proxy.SessionBinding{ProxyOrigin: resolvedProxyOrigin(resolved)}), nil
	case FormAuth:
		if strings.TrimSpace(supplier.FormToken) == "" {
			return nil, errors.New("sdk.New: FormAuth requires a non-empty FormToken")
		}
		return nil, errors.New("sdk.New: FormAuth requires the sessions reissue transport, which is not yet wired in the Go SDK")
	case SessionAuth:
		sessionOptions := SessionOptions{
			SessionID:     supplier.SessionID,
			SessionSecret: supplier.SessionSecret,
		}
		authOption, err := buildSessionOption(sessionOptions)
		if err != nil {
			return nil, err
		}
		zyinsOptions := append([]zyins.Option{authOption}, baseOptions...)
		zc, err := zyins.NewClient(zyinsOptions...)
		if err != nil {
			return nil, fmt.Errorf("sdk.New: zyins.NewClient: %w", err)
		}
		sessionID, sessionSecret := resolveSessionCredentials(sessionOptions)
		return newIsa(zc, nil, proxy.SessionBinding{
			SessionID:     sessionID,
			SessionSecret: sessionSecret,
			ProxyOrigin:   resolvedProxyOrigin(resolved),
		}), nil
	default:
		return nil, fmt.Errorf("sdk.New: unknown auth supplier kind %q", supplier.authKind())
	}
}

func normalizeAuthSupplier(auth AuthSupplier) (AuthSupplier, error) {
	switch supplier := auth.(type) {
	case *BearerAuth:
		if supplier == nil {
			return nil, errors.New("sdk.New: Auth must not be a nil *BearerAuth")
		}
		return *supplier, nil
	case *LicenseAuth:
		if supplier == nil {
			return nil, errors.New("sdk.New: Auth must not be a nil *LicenseAuth")
		}
		return *supplier, nil
	case *FormAuth:
		if supplier == nil {
			return nil, errors.New("sdk.New: Auth must not be a nil *FormAuth")
		}
		return *supplier, nil
	case *SessionAuth:
		if supplier == nil {
			return nil, errors.New("sdk.New: Auth must not be a nil *SessionAuth")
		}
		return *supplier, nil
	default:
		return auth, nil
	}
}

func normalizeEngine(engine Engine) (Engine, error) {
	switch selector := engine.(type) {
	case *RemoteEngine:
		if selector == nil {
			return nil, errors.New("sdk.New: Engine must not be a nil *RemoteEngine")
		}
		return *selector, nil
	case *LocalEngine:
		if selector == nil {
			return nil, errors.New("sdk.New: Engine must not be a nil *LocalEngine")
		}
		return *selector, nil
	case *ProxyEngine:
		if selector == nil {
			return nil, errors.New("sdk.New: Engine must not be a nil *ProxyEngine")
		}
		return *selector, nil
	case *InMemoryEngine:
		if selector == nil {
			return nil, errors.New("sdk.New: Engine must not be a nil *InMemoryEngine")
		}
		return *selector, nil
	default:
		return engine, nil
	}
}

func resolvedZyinsOptions(resolved ResolvedIsaOptions) []zyins.Option {
	return []zyins.Option{
		zyins.WithBaseURL(resolved.BaseURL),
		zyins.WithHTTPClient(&http.Client{
			Timeout: resolved.Timeout,
			Transport: headerTransport{
				inner:  http.DefaultTransport,
				header: apiVersionHeader,
				value:  string(resolved.APIVersion),
			},
		}),
	}
}

func resolvedProxyOrigin(resolved ResolvedIsaOptions) string {
	if resolved.ProxyOrigin != "" {
		return resolved.ProxyOrigin
	}
	return proxy.DefaultProxyOrigin
}

func resolveBearerToken(token string) (string, error) {
	if token != "" {
		return token, nil
	}
	envToken, ok := os.LookupEnv(zyins.EnvTokenVar)
	if !ok || envToken == "" {
		return "", &zyins.ConfigError{
			Factory:    "WithBearer",
			MissingEnv: []string{zyins.EnvTokenVar},
		}
	}
	return envToken, nil
}

type headerTransport struct {
	inner  http.RoundTripper
	header string
	value  string
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(t.header, t.value)
	return t.inner.RoundTrip(req)
}
