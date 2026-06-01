package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/isa-sdk/sdk/core/license"
)

// userAgentHeader matches the package version reported in CHANGELOG.md.
const userAgentHeader = "isa-sdk-account-go/0.4.0"

const jsonContentType = "application/json"
const idempotencyKeyLen = 16

// callArgs groups the inputs for one signed request.
type callArgs struct {
	method         string
	path           string
	body           []byte
	idempotencyKey string
}

// signedDo computes the License-HMAC headers, executes the request, and
// returns the raw response bytes. Non-2xx responses are surfaced as
// typed *HTTPError so callers can branch on Status / Code.
func (c *Client) signedDo(ctx context.Context, args callArgs) ([]byte, error) {
	headers, err := license.Build(license.Input{
		LicenseKey: c.auth.LicenseKey,
		OrderID:    c.auth.OrderID,
		Email:      c.auth.Email,
		Method:     args.method,
		RequestURI: args.path,
		Body:       args.body,
		DeviceID:   c.auth.DeviceID,
		Clock:      c.clock,
	})
	if err != nil {
		return nil, fmt.Errorf("account: signing %s %s: %w", args.method, args.path, err)
	}
	url := strings.TrimRight(c.baseURL, "/") + args.path
	var bodyReader io.Reader
	if len(args.body) > 0 {
		bodyReader = bytes.NewReader(args.body)
	}
	req, err := http.NewRequestWithContext(ctx, args.method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("account: building %s %s: %w", args.method, args.path, err)
	}
	for k, v := range headers.AsMap() {
		req.Header.Set(k, v)
	}
	if len(args.body) > 0 {
		req.Header.Set("Content-Type", jsonContentType)
	}
	req.Header.Set("Accept", jsonContentType)
	req.Header.Set("User-Agent", userAgentHeader)
	if needsIdempotency(args.method) {
		key := args.idempotencyKey
		if key == "" {
			key, err = generateIdempotencyKey()
			if err != nil {
				return nil, fmt.Errorf("account: %s %s idempotency key: %w", args.method, args.path, err)
			}
		}
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("account: %s %s: %w", args.method, args.path, err)
	}
	defer drainAndClose(resp.Body)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("account: reading %s %s response: %w", args.method, args.path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{Status: resp.StatusCode, Body: body, Method: args.method, Path: args.path}
	}
	return body, nil
}

func needsIdempotency(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func generateIdempotencyKey() (string, error) {
	buf := make([]byte, idempotencyKeyLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("account: failed to generate idempotency key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// HTTPError is the typed failure shape returned by signedDo for non-2xx
// responses. Body carries the raw server payload so callers can extract
// ProblemDetails fields without re-reading the network.
type HTTPError struct {
	Status int
	Method string
	Path   string
	Body   []byte
}

// Error renders the typed shape; the Body is truncated to keep log
// output bounded.
func (e *HTTPError) Error() string {
	preview := string(e.Body)
	if len(preview) > 256 {
		preview = preview[:256] + "..."
	}
	return fmt.Sprintf("account: %s %s returned HTTP %d: %s", e.Method, e.Path, e.Status, preview)
}

// drainAndClose consumes any unread bytes so the keep-alive connection
// returns to the pool.
func drainAndClose(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

// unwrapEnvelope returns the value under the top-level `data` key when
// the body is an envelope, or the parsed body itself when not. The
// caller-passed unmarshal target is left alone if the body is empty.
func unwrapEnvelope(body []byte) (json.RawMessage, error) {
	if len(body) == 0 {
		return nil, nil
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(body, &env); err != nil {
		// Not an object — treat the whole body as the payload.
		return body, nil
	}
	if data, ok := env["data"]; ok && len(data) > 0 && !bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return data, nil
	}
	return body, nil
}
