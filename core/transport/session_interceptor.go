// Provides: the transparent session interceptor that wraps every
// outbound product call with bootstrap-on-miss + retry-on-401.
//
// The interceptor sits between product packages (zyins, account,
// rapidsign, proxy) and the underlying HTTPDoer. Every product method
// already calls into transport.Doer; wiring SessionInterceptor as the
// outermost layer is the single insertion point that covers all
// methods without per-call changes.
//
// Behavior:
//  1. On every Do(req): read currentSecret. If nil, Bootstrap via the
//     session.Store (single-flight). Sign the request with the
//     sessionSecret + SignRequest helper. Forward.
//  2. On 401 with code=session_expired in the ProblemDetails body:
//     Invalidate + Bootstrap, replay the original request body once
//     with the new secret. Only one retry — a second 401 is returned
//     to the caller.
//
// The 30-second server-side grace overlap means a freshly-rotated
// secret is accepted alongside the previous one for 30 seconds, so the
// client doesn't need to track the prior secret across rotation.
package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/isa-sdk/sdk/core"
	"github.com/isa-sdk/sdk/core/session"
)

const maxProblemBodyBytes = 8 << 10

const (
	sessionExpiredCode = "session_expired"
	sessionRevokedCode = "session_revoked"
)

// SessionInterceptor wraps an inner HTTPDoer with auto-bootstrap and
// retry-on-401 logic. Construct via NewSessionInterceptor; safe for
// concurrent use.
type SessionInterceptor struct {
	store *session.Store
	inner HTTPDoer
}

// NewSessionInterceptor returns an interceptor. Both arguments
// required.
func NewSessionInterceptor(store *session.Store, inner HTTPDoer) (*SessionInterceptor, error) {
	if store == nil {
		return nil, errors.New("transport: NewSessionInterceptor requires a non-nil session.Store")
	}
	if inner == nil {
		return nil, errors.New("transport: NewSessionInterceptor requires a non-nil inner HTTPDoer")
	}
	return &SessionInterceptor{store: store, inner: inner}, nil
}

// Do signs and forwards the request. Returns the inner response
// unless a single 401 session_expired retry rebound succeeds.
func (s *SessionInterceptor) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("transport: nil request")
	}
	if req.URL == nil {
		return nil, errors.New("transport: nil request URL")
	}
	// Read body once so we can replay on 401. http.Request bodies are
	// single-shot; product callers don't expect us to consume theirs,
	// so we restore it before the first send.
	body, bodyErr := snapshotBody(req)
	if bodyErr != nil {
		return nil, fmt.Errorf("transport: snapshot body (path=%s): %w", req.URL.Path, bodyErr)
	}
	resp, sendErr := s.signAndSend(req, body)
	if sendErr != nil {
		return nil, fmt.Errorf("transport: signAndSend (path=%s): %w", req.URL.Path, sendErr)
	}
	code := sessionProblemCode(resp)
	if code == sessionRevokedCode {
		s.store.Invalidate()
		return resp, nil
	}
	if code != sessionExpiredCode {
		return resp, nil
	}
	// Drain + close before we replay so the underlying connection can
	// be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	s.store.Invalidate()
	return s.signAndSend(req, body)
}

func (s *SessionInterceptor) signAndSend(req *http.Request, body []byte) (*http.Response, error) {
	sess := s.store.CurrentSecret()
	if sess == nil {
		newSess, bsErr := s.store.Bootstrap(req.Context())
		if bsErr != nil {
			return nil, fmt.Errorf("transport: bootstrap session: %w", bsErr)
		}
		sess = newSess
	}
	signed, sigErr := core.SignRequest(core.SignRequestInput{
		Method:        req.Method,
		Path:          req.URL.Path,
		Body:          body,
		SessionID:     sess.ID,
		SessionSecret: sess.Secret,
	})
	if sigErr != nil {
		return nil, fmt.Errorf("transport: sign request (path=%s): %w", req.URL.Path, sigErr)
	}
	for k, v := range signed.AsMap() {
		req.Header.Set(k, v)
	}
	resetRequestBody(req, body)
	resp, doErr := s.inner.Do(req)
	if doErr != nil {
		return nil, fmt.Errorf("transport: inner Do (path=%s): %w", req.URL.Path, doErr)
	}
	return resp, nil
}

func snapshotBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	body, readErr := io.ReadAll(req.Body)
	if readErr != nil {
		return nil, fmt.Errorf("transport: read request body: %w", readErr)
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func resetRequestBody(req *http.Request, body []byte) {
	if body == nil {
		req.Body = nil
		req.GetBody = nil
		req.ContentLength = 0
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.ContentLength = int64(len(body))
}

// problemDetailsCode matches the RFC 7807 Problem Details `code`
// field. The server emits `code=session_expired` on 401 when the
// session has lapsed past its hard expiry + 30s grace.
type problemDetailsCode struct {
	Code string `json:"code"`
}

func sessionProblemCode(resp *http.Response) string {
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		return ""
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "json") {
		return ""
	}
	originalBody := resp.Body
	if originalBody == nil {
		return ""
	}
	b, readErr := io.ReadAll(io.LimitReader(originalBody, maxProblemBodyBytes))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(b), originalBody),
		Closer: originalBody,
	}
	if readErr != nil {
		return ""
	}
	var pd problemDetailsCode
	if decErr := json.Unmarshal(b, &pd); decErr != nil {
		return ""
	}
	return pd.Code
}

// Compile-time assertion that SessionInterceptor satisfies HTTPDoer.
var _ HTTPDoer = (*SessionInterceptor)(nil)
