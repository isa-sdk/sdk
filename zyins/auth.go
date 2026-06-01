package zyins

import (
	"errors"
	"strings"
)

// TokenSource returns the bearer token for the next outbound request.
// Implementations MAY refresh on demand; the SDK does not cache.
type TokenSource interface {
	Token() (string, error)
}

// StaticToken is a TokenSource that returns the same token forever.
// Suitable for short-lived programs and tests. Long-running services
// that rotate credentials should supply a custom TokenSource via
// WithTokenSource.
type StaticToken string

// Token returns the static token, or an error if empty or if the token
// carries leading/trailing whitespace. Surrounding whitespace is almost
// always an env-loading bug; trimming silently would mask it and lead
// to surprising 401s when the trimmed value diverges from what the
// caller stored elsewhere.
func (s StaticToken) Token() (string, error) {
	raw := string(s)
	if len(raw) == 0 {
		return "", errors.New("zyins: static token is empty")
	}
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", errors.New("zyins: static token is whitespace only")
	}
	if len(trimmed) != len(raw) {
		return "", errors.New("zyins: static token has leading or trailing whitespace; trim before passing to the SDK")
	}
	return raw, nil
}

// validTokenPrefixes lists the acceptable plaintext prefixes for ISA
// platform bearer tokens. See ADR on token migration; the legacy
// `zyins_*` form is intentionally not accepted by new SDK releases.
var validTokenPrefixes = []string{"isa_live_", "isa_test_"}

// validateTokenShape returns nil when token starts with an accepted
// prefix. The check is structural only — server-side validation is the
// authoritative gate; this catches obvious misconfiguration at
// construction time rather than per-request.
func validateTokenShape(token string) error {
	if len(token) == 0 {
		return errors.New("zyins: token is empty")
	}
	trimmed := strings.TrimSpace(token)
	if len(trimmed) == 0 {
		return errors.New("zyins: token is whitespace only")
	}
	if len(trimmed) != len(token) {
		return errors.New("zyins: token has leading or trailing whitespace; trim before passing to the SDK")
	}
	for _, p := range validTokenPrefixes {
		if strings.HasPrefix(token, p) {
			return nil
		}
	}
	return errors.New("zyins: token must start with isa_live_ or isa_test_")
}
