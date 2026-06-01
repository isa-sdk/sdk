// Package transport carries the hand-written client-side primitives that
// complement the protobuf-generated types in sdk/core.
//
// Bearer auth, retry-with-Retry-After backoff, and response envelope
// extraction are the three pieces every product SDK consumer needs but
// neither buf nor protoc-gen-* will emit. Keeping them here — rather
// than per-product — guarantees a single behavior across zyins,
// rapidsign, and proxy.
package transport

import (
	"errors"
	"net/http"
)

// HTTPDoer is the minimal HTTP client contract these helpers depend on.
// http.Client satisfies it; tests use an in-memory fake to assert
// header propagation without binding the network stack.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TokenSource returns the bearer credential for the next request.
// Implementations MAY refresh on demand; the helper does not cache.
type TokenSource interface {
	Token() (string, error)
}

// StaticToken is a TokenSource that returns the same token forever.
// Suitable for short-lived test programs and CI smoke runs. Production
// callers should use a refreshing implementation.
type StaticToken string

// Token returns the static token unchanged.
func (s StaticToken) Token() (string, error) {
	if s == "" {
		return "", errors.New("transport: static token is empty")
	}
	return string(s), nil
}

// ErrNilTokenSource and ErrNilInnerDoer are returned by NewBearerTransport
// when its required arguments are nil. Constructors return errors rather
// than panic so library callers can surface misconfiguration through
// normal error channels.
var (
	ErrNilTokenSource = errors.New("transport: NewBearerTransport requires a non-nil TokenSource")
	ErrNilInnerDoer   = errors.New("transport: NewBearerTransport requires a non-nil inner HTTPDoer")
)

// BearerTransport injects `Authorization: Bearer <token>` into every
// request before delegating to the inner client. It mirrors the
// http.RoundTripper composition pattern used by AWS-SDK-Go-v2 and the
// Stripe Go client.
type BearerTransport struct {
	source TokenSource
	inner  HTTPDoer
}

// NewBearerTransport returns a BearerTransport. Both arguments are
// required; nil values yield a typed error so misconfiguration surfaces
// at construction rather than per-request.
func NewBearerTransport(source TokenSource, inner HTTPDoer) (*BearerTransport, error) {
	if source == nil {
		return nil, ErrNilTokenSource
	}
	if inner == nil {
		return nil, ErrNilInnerDoer
	}
	return &BearerTransport{source: source, inner: inner}, nil
}

// Do attaches the bearer token and forwards to the inner client. The
// header is set unconditionally — any prior Authorization value is
// overwritten, matching how AWS SigV4 and Google ADC behave.
func (b *BearerTransport) Do(req *http.Request) (*http.Response, error) {
	token, err := b.source.Token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return b.inner.Do(req)
}
