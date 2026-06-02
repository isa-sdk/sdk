package account

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Zero-knowledge case share-link assembly + parsing, byte-compatible with the
// TypeScript SDK's caseWire.ts / cases.ts. The link is the capability: it
// carries the case code in the path and the decryption key in the `#k=`
// fragment. These helpers never log it.

// DefaultCaseViewerBaseURL is the default share-link viewer origin. The SDK
// appends `/<code>#k=<key>`; the base omits any path segment so a deployment
// can point it at any host without re-encoding the path shape.
const DefaultCaseViewerBaseURL = "https://link.isaapi.com"

// fragmentKeyPrefix delimits the path from the fragment key in a share link.
const fragmentKeyPrefix = "#k="

// legacyCaseRoute is the path segment used by links shared before the
// single-segment format; parseLink still accepts it.
const legacyCaseRoute = "c"

// ParsedLink is a case's code and fragment key, parsed out of a share link.
type ParsedLink struct {
	// Code is the case identifier from the link's last path segment.
	Code string
	// KeyFragment is the base64url data key from the `#k=` fragment.
	KeyFragment string
}

// AssembleLink builds `${base}/<code>#k=<keyFragment>`, stripping a trailing
// slash on the viewer base. The code is the only path segment added; any
// product prefix rides inside the configured base URL.
func AssembleLink(viewerBaseURL, code, keyFragment string) string {
	base := strings.TrimSuffix(viewerBaseURL, "/")
	return base + "/" + url.PathEscape(code) + fragmentKeyPrefix + keyFragment
}

// ParseLink parses a share link into its case code and fragment key. It
// accepts both the current single-segment shape (`{base}/<code>#k=<key>`) and
// the legacy `{base}/c/<id>#k=<key>` shape, so links shared before the format
// change keep opening. The code is the last non-empty path segment.
func ParseLink(link string) (ParsedLink, error) {
	if link == "" {
		return ParsedLink{}, errors.New("account: Cases.Open requires a non-empty link")
	}
	hashAt := strings.Index(link, fragmentKeyPrefix)
	if hashAt < 0 {
		return ParsedLink{}, errors.New("account: Cases.Open link is missing its #k= fragment key")
	}
	keyFragment := link[hashAt+len(fragmentKeyPrefix):]
	if keyFragment == "" {
		return ParsedLink{}, errors.New("account: Cases.Open link has an empty #k= fragment key")
	}
	code := lastPathSegment(link[:hashAt])
	if code == "" {
		return ParsedLink{}, errors.New("account: Cases.Open link must carry a case id before #k=<key>")
	}
	decoded, err := url.PathUnescape(code)
	if err != nil {
		return ParsedLink{}, fmt.Errorf("account: Cases.Open decode case id %q: %w", code, err)
	}
	return ParsedLink{Code: decoded, KeyFragment: keyFragment}, nil
}

// lastPathSegment returns the final non-empty `/`-delimited segment of path.
func lastPathSegment(path string) string {
	segments := strings.Split(path, "/")
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" {
			return segments[i]
		}
	}
	return ""
}

// ShareCreateInput is the request for Cases.Share: a routing product tag and
// the arbitrary JSON payload the SDK encrypts client-side before it leaves.
type ShareCreateInput struct {
	// Product is the routing tag stored cleartext and bound as AEAD data.
	Product string
	// Payload is the arbitrary JSON payload encrypted client-side.
	Payload any
}

// ShareCreateResult is the result of Cases.Share: the server-assigned case id
// and the assembled share link. The decryption key lives only in the link.
type ShareCreateResult struct {
	// ID is the server-assigned case id.
	ID string
	// Link is the full share link `${ViewerBaseURL}/<id>#k=<base64url(key)>`.
	Link string
}

// OpenResult is a decrypted case returned by Cases.Open.
type OpenResult struct {
	// Product is the routing tag the case was created under.
	Product string
	// Payload is the decrypted JSON payload.
	Payload any
}

// Share encrypts a payload client-side, stores the opaque envelope via
// POST /v1/case, and returns the fragment-keyed share link. The decryption
// key never reaches the server. The link is returned as a value and nothing
// else: it is never logged, never attached to a thrown error.
func (s *CasesService) Share(ctx context.Context, in ShareCreateInput, opts ...CallOption) (*ShareCreateResult, error) {
	if in.Product == "" {
		return nil, errors.New("account: Cases.Share requires a product")
	}
	if in.Payload == nil {
		return nil, errors.New("account: Cases.Share requires a payload")
	}
	encrypted, err := EncryptCase(in.Product, in.Payload, s.client.random)
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Share encrypt: %w", err)
	}
	body, err := marshalShareBody(in.Product, encrypted.Envelope)
	if err != nil {
		return nil, err
	}
	co := collectCallOptions(opts)
	respBody, err := s.client.signedDo(ctx, callArgs{
		method:         http.MethodPost,
		path:           casesPath,
		body:           body,
		idempotencyKey: co.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Share: %w", err)
	}
	id, err := parseSharedCaseID(respBody)
	if err != nil {
		return nil, err
	}
	return &ShareCreateResult{
		ID:   id,
		Link: AssembleLink(s.client.caseViewerBaseURL(), id, encrypted.KeyFragment),
	}, nil
}

// Open resolves a share link: it parses the code and fragment key, fetches the
// opaque envelope via GET /v1/case/{code}, and decrypts locally. The key comes
// only from the link the caller already holds.
func (s *CasesService) Open(ctx context.Context, link string) (*OpenResult, error) {
	parsed, err := ParseLink(link)
	if err != nil {
		return nil, err
	}
	path := casesPath + "/" + url.PathEscape(parsed.Code)
	respBody, err := s.client.signedDo(ctx, callArgs{method: http.MethodGet, path: path})
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Open: %w", err)
	}
	product, envelope, err := parseCaseDetail(respBody)
	if err != nil {
		return nil, err
	}
	payload, err := DecryptCase(product, envelope, parsed.KeyFragment)
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Open decrypt: %w", err)
	}
	return &OpenResult{Product: product, Payload: payload}, nil
}
