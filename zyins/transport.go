package zyins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	coretransport "github.com/isa-sdk/sdk/core/transport"
)

// httpDoer is the minimal HTTP contract the SDK depends on. The real
// dependency lives in sdk/core/transport.HTTPDoer; aliased here so
// internal call sites do not import the core package for the type
// name alone.
type httpDoer = coretransport.HTTPDoer

// userAgentHeader is the canonical SDK User-Agent string. The version
// suffix is fixed at build time; bump it on each release.
const userAgentHeader = "isa-sdk-zyins-go/0.5.5"

// jsonContentType is the wire media type for ZyINS requests and
// responses. The server accepts application/problem+json on errors
// only; clients always send application/json on the request side.
const jsonContentType = "application/json"

// requestArgs is the operation-level input to the SDK's HTTP helper.
// Grouping these into a struct keeps the helper signature narrow and
// lets new options (e.g., per-call timeouts) slot in without changing
// every caller.
type requestArgs struct {
	method string
	path   string
	body   any
	// op is the logical operation name (e.g., "prequalify", "quote",
	// "datasets_list"). Propagated into error messages so failures
	// originating in shared helpers still identify the caller.
	op             string
	idempotencyKey string
	// bootstrap routes the request through the unwrapped HTTP doer so no
	// Authorization header is attached. Used by the /v2/licenses/*
	// surface, which sits outside AuthMiddleware on the server.
	bootstrap bool
	// extraHeaders are merged onto the outbound request after the SDK's
	// canonical headers (Content-Type, Accept, Idempotency-Key, User-Agent).
	// Reserved for the bootstrap surface, which adds X-Device-ID without
	// otherwise touching auth.
	extraHeaders map[string]string
}

// doJSON serializes body, executes the request, and returns the
// response body bytes. Non-2xx responses are surfaced as typed errors
// via errorFromResponse; the caller never sees a raw 4xx/5xx body.
//
// The args.op label is included in every error message so a failed
// request from deep inside a generic helper (e.g., listDataset) still
// reports which logical operation triggered it.
func (c *Client) doJSON(ctx context.Context, args requestArgs) ([]byte, error) {
	out, _, err := c.doJSONRaw(ctx, args)
	return out, err
}

// doJSONRaw is doJSON with the *http.Response surfaced so callers that
// want headers/status can capture them via captureRawResponse. The
// response body is fully consumed before return; only metadata is
// preserved on the returned object.
func (c *Client) doJSONRaw(ctx context.Context, args requestArgs) ([]byte, *http.Response, error) {
	bodyBytes, err := marshalBody(args.body)
	if err != nil {
		return nil, nil, fmt.Errorf("zyins: %s %s [op=%s] body marshal: %w", args.method, args.path, args.op, err)
	}
	req, err := c.buildRequest(ctx, args, bodyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("zyins: %s %s [op=%s] build request: %w", args.method, args.path, args.op, err)
	}
	doer := c.doer
	if args.bootstrap && c.bootstrapDoer != nil {
		doer = c.bootstrapDoer
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("zyins: %s %s [op=%s]: %w", args.method, args.path, args.op, err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp, fmt.Errorf("zyins: %s %s [op=%s]: %w", args.method, args.path, args.op, errorFromResponse(resp))
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, fmt.Errorf("zyins: reading %s %s [op=%s] response: %w", args.method, args.path, args.op, err)
	}
	return out, resp, nil
}

// buildRequest assembles the *http.Request, attaching content-type,
// idempotency key, user-agent, and (when supplied) JSON body. The
// bearer token is injected by the wrapping BearerTransport.
func (c *Client) buildRequest(ctx context.Context, args requestArgs, body []byte) (*http.Request, error) {
	url := strings.TrimRight(c.baseURL, "/") + args.path
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, args.method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("zyins: failed to build %s %s request: %w", args.method, args.path, err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", jsonContentType)
	}
	req.Header.Set("Accept", jsonContentType)
	req.Header.Set("User-Agent", c.userAgent)
	if needsIdempotency(args.method) {
		key := args.idempotencyKey
		if key == "" {
			key, err = generateIdempotencyKey()
			if err != nil {
				return nil, fmt.Errorf("zyins: %s %s idempotency key: %w", args.method, args.path, err)
			}
		}
		req.Header.Set("Idempotency-Key", key)
	}
	for k, v := range args.extraHeaders {
		if v == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	return req, nil
}

// needsIdempotency reports whether a verb mutates server state and
// therefore should carry an Idempotency-Key header.
func needsIdempotency(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// marshalBody renders the operation body. nil bodies produce zero-byte
// output so GETs and DELETEs do not send `null`.
func marshalBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("zyins: failed to serialize request body: %w", err)
	}
	return out, nil
}

// drainAndClose discards the response body so the underlying TCP
// connection returns to the keep-alive pool.
func drainAndClose(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
