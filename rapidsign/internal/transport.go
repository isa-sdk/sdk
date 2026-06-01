package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Doer is the minimal HTTP contract the client depends on. The
// production wiring layers BearerTransport over RetryTransport over
// http.DefaultClient; tests substitute a fake.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RequestOptions configures a single outbound call. Headers extend
// (not replace) any defaults the inner transports add.
type RequestOptions struct {
	IdempotencyKey string
	Headers        map[string]string
	Query          map[string]string
}

// JSONRequest marshals body as JSON, executes the request through doer,
// and returns the response. The caller owns closing the body. Returning
// the raw response (rather than decoding here) keeps error
// classification at the call site where the typed error subclasses live.
func JSONRequest(
	ctx context.Context,
	doer Doer,
	method, url string,
	body any,
	opts RequestOptions,
) (*http.Response, error) {
	var (
		reader io.Reader
		getBody func() (io.ReadCloser, error)
	)
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("rapidsign: failed to marshal request body for %s %s: %w", method, url, err)
		}
		reader = bytes.NewReader(buf)
		// GetBody is required by the retry transport to rewind the body
		// on every attempt. We capture the encoded bytes once and hand
		// out fresh readers on demand.
		buf2 := buf
		getBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(buf2)), nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("rapidsign: failed to construct %s %s: %w", method, url, err)
	}
	if getBody != nil {
		req.GetBody = getBody
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if opts.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	if len(opts.Query) > 0 {
		q := req.URL.Query()
		for k, v := range opts.Query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rapidsign: transport %s %s failed: %w", method, url, err)
	}
	return resp, nil
}
