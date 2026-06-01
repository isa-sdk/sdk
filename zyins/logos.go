package zyins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// logosPath is the canonical path prefix for the carrier-logo asset
// endpoint. The server also accepts the legacy singular form
// (/v1/logo/{carrier}) — both routes resolve to the same handler since
// zyins #303 merged. The SDK uses the canonical plural per api-standards.
const logosPath = "/v1/logo"

// LogosGetOptions captures the optional inputs for Logos.Get.
type LogosGetOptions struct {
	// DataURI requests a `data:image/...;base64,...` text body instead
	// of raw image bytes. The server toggles the response shape via the
	// `?ds=true` query parameter; the SDK validates the prefix to catch
	// proxy misconfigurations.
	DataURI bool
}

// LogosGetOption is the functional option for Logos.Get.
type LogosGetOption func(*LogosGetOptions)

// WithDataURI requests the data-URI response shape. Equivalent to
// passing `LogosGetOptions{DataURI: true}`.
func WithDataURI(on bool) LogosGetOption {
	return func(o *LogosGetOptions) { o.DataURI = on }
}

// LogosService groups the public carrier-logo asset endpoint. The
// endpoint is non-credentialed per api-standards.md (GET allowlist), so
// the SDK does NOT attach License-HMAC or bearer headers to these calls.
type LogosService struct {
	client *Client
}

// Get fetches the carrier-logo asset.
//
// With no options, the call returns the raw image bytes (typically PNG
// or JPEG). With WithDataURI(true), the call returns a
// `data:image/...;base64,...` string suitable for inline HTML / CSS
// embedding.
//
// The two-return signature is a Go idiomatic alternative to the TS
// overload: callers see `(bytes, dataURI, error)` and pick the one that
// matches their request. The unused field is always the zero value.
func (s *LogosService) Get(ctx context.Context, carrier string, opts ...LogosGetOption) ([]byte, string, error) {
	if strings.TrimSpace(carrier) == "" {
		return nil, "", errors.New("zyins: Logos.Get requires a non-empty carrier")
	}
	o := LogosGetOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	path := logosPath + "/" + url.PathEscape(carrier)
	if o.DataURI {
		path += "?ds=true"
	}
	endpoint := strings.TrimRight(s.client.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("zyins: Logos.Get build request: %w", err)
	}
	req.Header.Set("User-Agent", s.client.userAgent)
	if o.DataURI {
		req.Header.Set("Accept", "text/plain")
	} else {
		req.Header.Set("Accept", "image/*")
	}
	resp, err := s.client.doer.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("zyins: Logos.Get %s: %w", endpoint, err)
	}
	defer drainAndClose(resp.Body)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("zyins: Logos.Get reading body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("zyins: Logos.Get %s: %w", endpoint, errorFromBytes(resp.StatusCode, body))
	}
	if o.DataURI {
		dataURI := string(body)
		if !strings.HasPrefix(dataURI, "data:image/") {
			preview := dataURI
			if len(preview) > 32 {
				preview = preview[:32]
			}
			return nil, "", fmt.Errorf("zyins: Logos.Get expected data:image/... URI but got: %s", preview)
		}
		return nil, dataURI, nil
	}
	return body, "", nil
}

// errorFromBytes wraps a non-2xx body in the typed Error funnel without
// requiring an *http.Response. Centralized here so logos and any future
// non-credentialed endpoints share one decoder.
func errorFromBytes(status int, body []byte) error {
	resp := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body)))}
	return errorFromResponse(resp)
}
