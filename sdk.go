// Package sdk is the unified ISA SDK for Go.
//
// It provides one client type, [Isa], with product namespaces attached
// as fields:
//
//	isa, err := sdk.WithBearer("isa_live_…")
//	if err != nil { return err }
//	resp, err := isa.Zyins.Prequalify.Run(ctx, req)
//
// The factory functions read environment-variable defaults when called
// with empty arguments (see SDK_DESIGN.md §3.3):
//
//	ISA_TOKEN                 → WithBearer
//	ISA_LICENSE_KEYCODE/EMAIL → WithLicense
//	ISA_SESSION_ID/SECRET     → WithSession
//
// All Phase 1-5 capabilities — stderr-only debug logging, typed
// [zyins.IdempotencyConflictError], envelope fields (RequestID,
// IdempotencyKey, RetryAttempts), *WithRawResponse variants, cursor
// escape hatch on iter structs — live in the product sub-packages and
// are reached via the namespaces below.
//
// Example:
//
//	isa, err := sdk.WithBearer("")  // reads ISA_TOKEN
//	if err != nil { return err }
//	resp, err := isa.Zyins.Prequalify.Run(ctx, &zyins.PrequalifyInput{...})
package sdk

import (
	"context"
	"fmt"
	"os"

	"github.com/isa-sdk/sdk/proxy"
	"github.com/isa-sdk/sdk/rapidsign"
	"github.com/isa-sdk/sdk/zyins"
	zcases "github.com/isa-sdk/sdk/zyins/cases"
)

// BundledAPIVersions re-exports [zyins.BundledAPIVersions] at the
// parent SDK entry point. Callers reading the bundled pin without
// importing the zyins sub-package go through this alias.
var BundledAPIVersions = zyins.BundledAPIVersions

// ResolveAPIVersion re-exports [zyins.ResolveAPIVersion] for callers
// that need per-surface resolution without importing the zyins
// sub-package directly.
func ResolveAPIVersion(overrides map[string]string, surface string) string {
	return zyins.ResolveAPIVersion(overrides, surface)
}

// Isa is the unified entry point. Construct one per process via the
// factory functions below; the namespaces share underlying transport
// resources and are safe for concurrent use.
type Isa struct {
	Zyins     *zyins.Client
	RapidSign *rapidsign.Client
	Webhooks  *WebhooksNamespace
	// Proxy exposes proxy.Call. Available only when the Isa was
	// constructed from a session credential (WithSession); other factories
	// leave it non-nil but its Call method returns *zyins.ConfigError at
	// the boundary so callers see the exchange-credentials hint.
	Proxy *ProxyNamespace
}

// ProxyNamespace is the proxy.Call entry point reached via isa.Proxy.
// The namespace carries the session binding for the parent Isa; the
// underlying transport is reconstructed per-call (stateless).
type ProxyNamespace struct {
	binding proxy.SessionBinding
}

// Call invokes a registered integration through the platform proxy.
// See proxy.Call for the full semantic contract.
func (n *ProxyNamespace) Call(ctx context.Context, opts proxy.CallOptions) ([]byte, error) {
	return proxy.Call(ctx, n.binding, opts)
}

// WebhooksNamespace is the placeholder for cross-product webhook
// helpers; per-product webhook verifiers continue to live on their
// product namespaces today.
type WebhooksNamespace struct{}

// LicenseOptions configures the License auth mode. Empty fields are
// filled from the environment (ISA_LICENSE_KEYCODE, ISA_LICENSE_EMAIL).
//
// APIVersion overrides the bundled per-surface pins
// ([zyins.BundledAPIVersions]) for this client instance only. Keys are
// surface names ("prequalify", "quote", ...); values are the
// version prefix ("v1", "v2", ...). A surface absent from the map
// falls back to BundledAPIVersions.
//
// CaseStorage swaps the default zero-knowledge store. nil resolves to
// [zcases.NewZeroKnowledgeCaseStorage] wrapping the underlying zyins
// client.
type LicenseOptions struct {
	Keycode     string
	Email       string
	APIVersion  map[string]string
	CaseStorage zcases.CaseStorage
}

// BearerOptions configures the Bearer auth mode. Token follows the
// same env fallback as [WithBearer] (ISA_TOKEN). APIVersion and
// CaseStorage mirror the per-instance overrides on [LicenseOptions].
type BearerOptions struct {
	Token       string
	APIVersion  map[string]string
	CaseStorage zcases.CaseStorage
}

// SessionOptions configures the Session auth mode. Empty fields are
// filled from the environment (ISA_SESSION_ID, ISA_SESSION_SECRET).
//
// APIVersion and CaseStorage mirror the per-instance overrides on
// [LicenseOptions].
type SessionOptions struct {
	SessionID     string
	SessionSecret string //nolint:gosec // documented credential field
	APIVersion    map[string]string
	CaseStorage   zcases.CaseStorage
}

// WithBearer constructs an Isa client authenticated by a long-lived
// bearer token. When the token argument is empty, ISA_TOKEN is read
// from the environment; absence returns *zyins.ConfigError.
//
// Example:
//
//	isa, err := sdk.WithBearer("")
//	if err != nil { return err }
//	resp, err := isa.Zyins.Prequalify.Run(ctx, req)
func WithBearer(token string) (*Isa, error) {
	return WithBearerOptions(BearerOptions{Token: token})
}

// WithBearerOptions is the options-bag form of [WithBearer]. The
// embedded APIVersion + CaseStorage fields propagate into the
// underlying zyins client via [zyins.WithAPIVersionOverrides] and
// [zyins.WithCaseStorage].
func WithBearerOptions(opts BearerOptions) (*Isa, error) {
	token, err := resolveBearerToken(opts.Token)
	if err != nil {
		return nil, err
	}
	zyinsOpts := append([]zyins.Option{zyins.WithToken(token)}, optionalZyinsOptions(opts.APIVersion, opts.CaseStorage)...)
	zc, err := zyins.NewClient(zyinsOpts...)
	if err != nil {
		return nil, fmt.Errorf("sdk: WithBearer: zyins.NewClient: %w", err)
	}
	rc, err := rapidsign.New(token)
	if err != nil {
		return nil, fmt.Errorf("sdk: WithBearer: rapidsign.New: %w", err)
	}
	return newIsa(zc, rc, proxy.SessionBinding{ProxyOrigin: proxy.DefaultProxyOrigin}), nil
}

// optionalZyinsOptions translates the per-instance APIVersion +
// CaseStorage fields shared across [BearerOptions], [LicenseOptions],
// and [SessionOptions] into the matching [zyins.Option] values. Empty
// inputs produce no options.
func optionalZyinsOptions(apiVersion map[string]string, storage zcases.CaseStorage) []zyins.Option {
	out := make([]zyins.Option, 0, 2)
	if len(apiVersion) > 0 {
		out = append(out, zyins.WithAPIVersionOverrides(apiVersion))
	}
	if storage != nil {
		out = append(out, zyins.WithCaseStorage(storage))
	}
	return out
}

// WithLicense constructs an Isa client authenticated by the License
// auth mode. Empty option fields are filled from the environment.
//
// License transport is not wired yet: valid credentials still return
// *zyins.ConfigError explaining the pending transport (same as
// [zyins.WithLicenseCredential]). Use [WithBearer] until it ships.
//
// Example:
//
//	isa, err := sdk.WithLicense(sdk.LicenseOptions{})  // reads env
//	if err != nil { return err }
func WithLicense(opts LicenseOptions) (*Isa, error) {
	zyinsAuthOpt, err := buildLicenseOption(opts)
	if err != nil {
		return nil, err
	}
	zyinsOpts := append([]zyins.Option{zyinsAuthOpt}, optionalZyinsOptions(opts.APIVersion, opts.CaseStorage)...)
	zc, err := zyins.NewClient(zyinsOpts...)
	if err != nil {
		return nil, fmt.Errorf("sdk: WithLicense: zyins.NewClient: %w", err)
	}
	return newIsa(zc, nil, proxy.SessionBinding{ProxyOrigin: proxy.DefaultProxyOrigin}), nil
}

// WithSession constructs an Isa client authenticated by the Session
// auth mode. Empty option fields are filled from the environment.
//
// Session transport is not wired yet: valid credentials still return
// *zyins.ConfigError explaining the pending transport (same as
// [zyins.WithSessionCredential]). Use [WithBearer] until it ships.
//
// Example:
//
//	isa, err := sdk.WithSession(sdk.SessionOptions{})  // reads env
//	if err != nil { return err }
func WithSession(opts SessionOptions) (*Isa, error) {
	zyinsAuthOpt, err := buildSessionOption(opts)
	if err != nil {
		return nil, err
	}
	zyinsOpts := append([]zyins.Option{zyinsAuthOpt}, optionalZyinsOptions(opts.APIVersion, opts.CaseStorage)...)
	zc, err := zyins.NewClient(zyinsOpts...)
	if err != nil {
		return nil, fmt.Errorf("sdk: WithSession: zyins.NewClient: %w", err)
	}
	sessionID, sessionSecret := resolveSessionCredentials(opts)
	return newIsa(zc, nil, proxy.SessionBinding{
		SessionID:     sessionID,
		SessionSecret: sessionSecret,
		ProxyOrigin:   proxy.DefaultProxyOrigin,
	}), nil
}

func newIsa(zc *zyins.Client, rc *rapidsign.Client, binding proxy.SessionBinding) *Isa {
	return &Isa{
		Zyins:     zc,
		RapidSign: rc,
		Webhooks:  &WebhooksNamespace{},
		Proxy:     &ProxyNamespace{binding: binding},
	}
}

// resolveSessionCredentials extracts the session id + secret from opts,
// falling back to env. Used by WithSession to populate the proxy binding
// without duplicating buildSessionOption's env logic.
func resolveSessionCredentials(opts SessionOptions) (string, string) {
	id := opts.SessionID
	if len(id) == 0 {
		id = os.Getenv(zyins.EnvSessionIDVar)
	}
	secret := opts.SessionSecret
	if len(secret) == 0 {
		secret = os.Getenv(zyins.EnvSessionSecretVar)
	}
	return id, secret
}

func buildLicenseOption(opts LicenseOptions) (zyins.Option, error) {
	keycode := opts.Keycode
	email := opts.Email
	missing := make([]string, 0, 2)
	if len(keycode) == 0 {
		if v := os.Getenv(zyins.EnvLicenseKeycodeVar); len(v) > 0 {
			keycode = v
		} else {
			missing = append(missing, zyins.EnvLicenseKeycodeVar)
		}
	}
	if len(email) == 0 {
		if v := os.Getenv(zyins.EnvLicenseEmailVar); len(v) > 0 {
			email = v
		} else {
			missing = append(missing, zyins.EnvLicenseEmailVar)
		}
	}
	if len(missing) > 0 {
		return nil, &zyins.ConfigError{Factory: "WithLicense", MissingEnv: missing}
	}
	return zyins.WithLicenseCredential(zyins.LicenseCredential{Keycode: keycode, Email: email}), nil
}

func buildSessionOption(opts SessionOptions) (zyins.Option, error) {
	id := opts.SessionID
	secret := opts.SessionSecret
	missing := make([]string, 0, 2)
	if len(id) == 0 {
		if v := os.Getenv(zyins.EnvSessionIDVar); len(v) > 0 {
			id = v
		} else {
			missing = append(missing, zyins.EnvSessionIDVar)
		}
	}
	if len(secret) == 0 {
		if v := os.Getenv(zyins.EnvSessionSecretVar); len(v) > 0 {
			secret = v
		} else {
			missing = append(missing, zyins.EnvSessionSecretVar)
		}
	}
	if len(missing) > 0 {
		return nil, &zyins.ConfigError{Factory: "WithSession", MissingEnv: missing}
	}
	return zyins.WithSessionCredential(zyins.SessionCredential{SessionID: id, SessionSecret: secret}), nil
}
