// Package session provides the steady-state session module that pairs
// with the bootstrap HMAC algorithm in core.BuildBootstrapSignature.
//
// The consumer never calls these directly: the auto-refresh interceptor
// in core/transport reads CurrentSecret on every outbound call,
// triggers Bootstrap on miss/expiry, and re-tries on 401
// session_expired. This module is the atomic-store half of that flow.
//
// Concurrency model:
//   - sync.RWMutex protects the cached Session.
//   - golang.org/x/sync/singleflight ensures concurrent Bootstrap calls
//     during expiry collapse to a single HTTP round-trip.
//   - The 30-second grace overlap lives on the server (see
//     services/account/internal/handler/sessions_bootstrap.go); the
//     client just retries on 401 and never tracks the previous secret.
package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/isa-sdk/sdk/core"
)

// Session is the cached credential bundle returned by POST /v1/sessions.
type Session struct {
	// ID is the short-lived session identifier used when signing requests.
	ID string
	// Secret is the short-lived HMAC key used for steady-state requests.
	Secret string
	// ExpiresAt is the hard expiry returned by POST /v1/sessions.
	ExpiresAt time.Time
}

// ExchangeInput groups the inputs the bootstrap signature needs.
type ExchangeInput struct {
	// Keycode is the per-seat keycode.
	Keycode string
	// Email is the license-owner email.
	Email string
	// LicenseKey is the long-lived bootstrap HMAC key.
	LicenseKey string
	// DeviceID is the stable per-install device identifier.
	DeviceID string
}

// Errors surfaced by the session module.
var (
	ErrEmptyExchangeInput = errors.New("session: ExchangeInput requires non-empty keycode, email, licenseKey, deviceId")
	ErrBootstrapFailed    = errors.New("session: POST /v1/sessions returned non-2xx")
)

const maxBootstrapErrorBodyBytes = 4 << 10

// HTTPDoer mirrors core/transport.HTTPDoer.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Clock is the time facade. Tests inject FixedClock; production uses SystemClock.
type Clock interface {
	Now() time.Time
}

// SystemClock returns time.Now().UTC().
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// Store is the thread-safe atomic cache + single-flight bootstrap driver.
type Store struct {
	doer            HTTPDoer
	clock           Clock
	baseURL         string
	input           ExchangeInput
	mu              sync.RWMutex
	current         *Session
	group           singleflight.Group
	proactiveWindow time.Duration
}

// NewStore constructs a Store. All arguments are required.
func NewStore(doer HTTPDoer, clock Clock, baseURL string, input ExchangeInput) (*Store, error) {
	if doer == nil {
		return nil, errors.New("session: NewStore requires a non-nil HTTPDoer")
	}
	if clock == nil {
		clock = SystemClock{}
	}
	if baseURL == "" {
		return nil, errors.New("session: NewStore requires a non-empty baseURL")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("session: NewStore requires a non-empty baseURL")
	}
	if input.Keycode == "" || input.Email == "" || input.LicenseKey == "" || input.DeviceID == "" {
		return nil, ErrEmptyExchangeInput
	}
	return &Store{
		doer:            doer,
		clock:           clock,
		baseURL:         baseURL,
		input:           input,
		proactiveWindow: 5 * time.Minute,
	}, nil
}

// CurrentSecret returns the cached session if present and not past expiry.
func (s *Store) CurrentSecret() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil
	}
	if !s.current.ExpiresAt.IsZero() && !s.clock.Now().Before(s.current.ExpiresAt) {
		return nil
	}
	cp := *s.current
	return &cp
}

// Bootstrap performs POST /v1/sessions with the embedded HMAC signature.
// Concurrent calls share one round-trip via singleflight.
func (s *Store) Bootstrap(ctx context.Context) (*Session, error) {
	ch := s.group.DoChan("bootstrap", func() (any, error) {
		sess, exchangeErr := s.doExchange(context.WithoutCancel(ctx))
		if exchangeErr != nil {
			return nil, exchangeErr
		}
		s.mu.Lock()
		s.current = sess
		s.mu.Unlock()
		return sess, nil
	})
	var v any
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, fmt.Errorf("session: Bootstrap (keycode=%s): %w", s.input.Keycode, res.Err)
		}
		v = res.Val
	case <-ctx.Done():
		return nil, fmt.Errorf("session: Bootstrap canceled (keycode=%s): %w", s.input.Keycode, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("session: Bootstrap canceled (keycode=%s): %w", s.input.Keycode, err)
	}
	sess := v.(*Session)
	cp := *sess
	return &cp, nil
}

// Invalidate clears the cached session. Called on 401 session_expired.
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
}

// OnActivity is the consumer-facing proactive refresh hook.
func (s *Store) OnActivity(ctx context.Context) error {
	s.mu.RLock()
	needsBootstrap := s.current == nil || !s.clock.Now().Add(s.proactiveWindow).Before(s.current.ExpiresAt)
	s.mu.RUnlock()
	if !needsBootstrap {
		return nil
	}
	if s.CurrentSecret() == nil {
		if _, bsErr := s.Bootstrap(ctx); bsErr != nil {
			return fmt.Errorf("session: OnActivity initial bootstrap (keycode=%s): %w", s.input.Keycode, bsErr)
		}
		return nil
	}
	if _, bsErr := s.Bootstrap(ctx); bsErr != nil {
		return fmt.Errorf("session: OnActivity proactive refresh (keycode=%s): %w", s.input.Keycode, bsErr)
	}
	return nil
}

type bootstrapResponseBody struct {
	Data bootstrapResponseData `json:"data"`
}

type bootstrapResponseData struct {
	SessionID     string `json:"sessionId"`
	SessionSecret string `json:"sessionSecret"`
	ExpiresAt     string `json:"expiresAt"`
}

func (s *Store) doExchange(ctx context.Context) (*Session, error) {
	ts := s.clock.Now().Unix()
	sig, sigErr := core.BuildBootstrapSignature(core.BootstrapInput{
		Keycode:    s.input.Keycode,
		Email:      s.input.Email,
		LicenseKey: s.input.LicenseKey,
		DeviceID:   s.input.DeviceID,
		Method:     "POST",
		Path:       "/v1/sessions",
		Timestamp:  ts,
	})
	if sigErr != nil {
		return nil, fmt.Errorf("session: bootstrap signature (keycode=%s): %w", s.input.Keycode, sigErr)
	}
	req, reqErr := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/v1/sessions", bytes.NewReader([]byte(sig.SerializedBody)))
	if reqErr != nil {
		return nil, fmt.Errorf("session: build request (baseURL=%s): %w", s.baseURL, reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-ID", s.input.DeviceID)
	req.Header.Set("ISA-Signature", fmt.Sprintf("t=%d,v1=%s", ts, sig.Hex))
	resp, doErr := s.doer.Do(req)
	if doErr != nil {
		return nil, fmt.Errorf("session: POST /v1/sessions (baseURL=%s): %w", s.baseURL, doErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBootstrapErrorBodyBytes))
		return nil, fmt.Errorf("%w: status=%d body=%s", ErrBootstrapFailed, resp.StatusCode, string(b))
	}
	var body bootstrapResponseBody
	if decErr := json.NewDecoder(resp.Body).Decode(&body); decErr != nil {
		return nil, fmt.Errorf("session: decode response (status=%d): %w", resp.StatusCode, decErr)
	}
	if body.Data.SessionID == "" || body.Data.SessionSecret == "" {
		return nil, errors.New("session: bootstrap response missing sessionId or sessionSecret")
	}
	exp, parseErr := time.Parse(time.RFC3339, body.Data.ExpiresAt)
	if parseErr != nil {
		return nil, fmt.Errorf("session: parse expiresAt %q: %w", body.Data.ExpiresAt, parseErr)
	}
	return &Session{ID: body.Data.SessionID, Secret: body.Data.SessionSecret, ExpiresAt: exp}, nil
}
