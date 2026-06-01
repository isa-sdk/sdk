// Package zyins — environment-variable facade and credential factories.
//
// System boundaries (process env) go through an injectable facade so
// tests can replace the lookup without mutating os.Setenv on a parallel
// test binary, and so the rest of the SDK never imports os directly.
//
// Each factory in this file consults envReader for its defaults; a
// missing value yields *ConfigError with a clear, actionable message
// that names the env var the caller needs to set.

package zyins

import (
	"errors"
	"fmt"
	"os"
)

// Env var names consulted by the per-mode factories. Centralized so a
// caller-facing rename surfaces in one place rather than scattered
// string literals.
const (
	EnvTokenVar          = "ISA_TOKEN"
	EnvLicenseKeycodeVar = "ISA_LICENSE_KEYCODE"
	EnvLicenseEmailVar   = "ISA_LICENSE_EMAIL"
	EnvSessionIDVar      = "ISA_SESSION_ID"
	EnvSessionSecretVar  = "ISA_SESSION_SECRET" //nolint:gosec // env var name, not a secret value
)

// envReader is the minimal contract for environment-variable lookup.
// os-backed reader is the default; tests substitute a map-backed one
// rather than mutating process state.
type envReader interface {
	get(key string) (string, bool)
}

// osEnvReader is the default envReader backed by os.LookupEnv. Empty
// strings are treated as missing — a variable that is set but empty is
// indistinguishable from one the operator forgot to populate and should
// fail with the same actionable error.
type osEnvReader struct{}

func (osEnvReader) get(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// defaultEnv is the process-wide reader. Overridden in tests through
// withEnvReader (test-only export below).
var defaultEnv envReader = osEnvReader{}

// ConfigError is the typed error returned by the env-reading factories
// when one or more required variables are absent. Callers can match it
// with errors.As to surface a structured "you forgot to set X" message.
type ConfigError struct {
	// Factory names the constructor that failed (e.g., "WithBearer").
	Factory string
	// MissingEnv lists every env var that was unset or empty.
	MissingEnv []string
	// Detail is an optional free-text amplification for non-env causes.
	Detail string
}

// Error returns the human-readable message naming both the factory and
// every missing env var so the consumer immediately sees what to set.
func (e *ConfigError) Error() string {
	if e == nil {
		return "<nil zyins.ConfigError>"
	}
	if len(e.MissingEnv) == 0 {
		if e.Detail != "" {
			return fmt.Sprintf("zyins: %s: %s", e.Factory, e.Detail)
		}
		return fmt.Sprintf("zyins: %s: misconfigured", e.Factory)
	}
	if len(e.MissingEnv) == 1 {
		return fmt.Sprintf(
			"zyins: %s: required env var %q is not set; either pass the value explicitly or export %s in the environment",
			e.Factory, e.MissingEnv[0], e.MissingEnv[0])
	}
	return fmt.Sprintf(
		"zyins: %s: required env vars are not set: %v; either pass the values explicitly or export them in the environment",
		e.Factory, e.MissingEnv)
}

// IsaError is implemented by every error this SDK returns. Callers
// switch on this interface when they want code/request-id without
// committing to a specific concrete type.
type IsaError interface {
	error
	// IsaCode returns the stable wire enum (or empty for client-side
	// errors like *ConfigError that never reach the server).
	IsaCode() ErrorCode
}

// IsaCode returns ErrorCodeUnspecified — *ConfigError is a client-side
// misconfiguration that never hits the server.
func (e *ConfigError) IsaCode() ErrorCode { return ErrorCodeUnspecified }

// LicenseCredential captures the keycode + email pair the License auth
// mode signs requests with. Construction-time only; the value is opaque
// to the caller.
type LicenseCredential struct {
	Keycode string
	Email   string
}

// SessionCredential captures the session id + signing secret the
// Session auth mode signs requests with.
type SessionCredential struct {
	SessionID     string
	SessionSecret string //nolint:gosec // documented credential field; consumers expect this name
}

// WithBearer reads ISA_TOKEN from the environment and returns an Option
// that wires a static bearer token. Missing or empty env value returns
// *ConfigError in the second slot; the caller MUST check.
//
// Two-line hello world:
//
//	opt, err := zyins.WithBearer()
//	if err != nil { return err }
//	client, err := zyins.NewClient(opt)
func WithBearer() (Option, error) {
	return withBearerFrom(defaultEnv)
}

// withBearerFrom is the testable inner form of WithBearer; tests inject
// a map-backed envReader through it.
func withBearerFrom(env envReader) (Option, error) {
	token, ok := env.get(EnvTokenVar)
	if !ok {
		return nil, &ConfigError{
			Factory:    "WithBearer",
			MissingEnv: []string{EnvTokenVar},
		}
	}
	return WithToken(token), nil
}

// WithLicense reads ISA_LICENSE_KEYCODE and ISA_LICENSE_EMAIL from the
// environment and returns an Option that captures the credential.
// Missing values return *ConfigError naming every absent variable.
//
// The License auth mode is part of the unified runtime model (see
// SDK_DESIGN.md §3); at the time this factory is wired the License
// transport may still be pending. NewClient will reject License
// credentials with a clear error until the transport ships, so callers
// see the gap at construction rather than at first request.
func WithLicense() (Option, error) {
	return withLicenseFrom(defaultEnv)
}

func withLicenseFrom(env envReader) (Option, error) {
	keycode, kOK := env.get(EnvLicenseKeycodeVar)
	email, eOK := env.get(EnvLicenseEmailVar)
	missing := make([]string, 0, 2)
	if !kOK {
		missing = append(missing, EnvLicenseKeycodeVar)
	}
	if !eOK {
		missing = append(missing, EnvLicenseEmailVar)
	}
	if len(missing) > 0 {
		return nil, &ConfigError{Factory: "WithLicense", MissingEnv: missing}
	}
	return withLicenseCredential(LicenseCredential{Keycode: keycode, Email: email}), nil
}

// WithSession reads ISA_SESSION_ID and ISA_SESSION_SECRET from the
// environment and returns an Option that captures the credential. Same
// rules as WithLicense.
func WithSession() (Option, error) {
	return withSessionFrom(defaultEnv)
}

func withSessionFrom(env envReader) (Option, error) {
	id, iOK := env.get(EnvSessionIDVar)
	secret, sOK := env.get(EnvSessionSecretVar)
	missing := make([]string, 0, 2)
	if !iOK {
		missing = append(missing, EnvSessionIDVar)
	}
	if !sOK {
		missing = append(missing, EnvSessionSecretVar)
	}
	if len(missing) > 0 {
		return nil, &ConfigError{Factory: "WithSession", MissingEnv: missing}
	}
	return withSessionCredential(SessionCredential{SessionID: id, SessionSecret: secret}), nil
}

// errLicenseTransportPending and errSessionTransportPending are
// returned by NewClient when a caller wires a License/Session
// credential before the matching transport is available. The auth
// modes are part of the unified runtime model (SDK_DESIGN.md §3) but
// are scheduled to wire after the bearer-only baseline ships. The
// errors are typed *ConfigError so the same matching path applies.
var (
	errLicenseTransportPending = &ConfigError{
		Factory: "NewClient",
		Detail:  "License auth mode is captured but transport wiring is pending; use WithToken/WithBearer for now",
	}
	errSessionTransportPending = &ConfigError{
		Factory: "NewClient",
		Detail:  "Session auth mode is captured but transport wiring is pending; use WithToken/WithBearer for now",
	}
)

// WithLicenseCredential is the explicit-credential counterpart to
// [WithLicense]. The env-reading factory delegates to this one after it
// resolves the keycode and email from the environment.
//
// Example:
//
//	opt := zyins.WithLicenseCredential(zyins.LicenseCredential{
//	    Keycode: "ABC-123-XYZ",
//	    Email:   "agent@example.com",
//	})
//	client, err := zyins.NewClient(opt)
func WithLicenseCredential(cred LicenseCredential) Option {
	return withLicenseCredential(cred)
}

// WithSessionCredential is the explicit-credential counterpart to
// [WithSession]. The env-reading factory delegates to this one after it
// resolves the session id and secret from the environment.
//
// Example:
//
//	opt := zyins.WithSessionCredential(zyins.SessionCredential{
//	    SessionID:     "ses_01H…",
//	    SessionSecret: "…",
//	})
//	client, err := zyins.NewClient(opt)
func WithSessionCredential(cred SessionCredential) Option {
	return withSessionCredential(cred)
}

// withLicenseCredential returns an Option that stashes the credential
// on the options block. NewClient rejects it with errLicenseTransportPending
// until the License transport ships; structuring it this way means the
// env-reading surface lands cleanly and the transport wiring lands as a
// pure addition.
func withLicenseCredential(cred LicenseCredential) Option {
	return func(o *options) error {
		if cred.Keycode == "" || cred.Email == "" {
			return errors.New("zyins: WithLicense requires non-empty keycode and email")
		}
		o.license = &cred
		return nil
	}
}

// withSessionCredential is the Session counterpart to withLicenseCredential.
func withSessionCredential(cred SessionCredential) Option {
	return func(o *options) error {
		if cred.SessionID == "" || cred.SessionSecret == "" {
			return errors.New("zyins: WithSession requires non-empty session id and secret")
		}
		o.session = &cred
		return nil
	}
}
