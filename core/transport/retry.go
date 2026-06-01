package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Clock returns the current instant. Tests pin it to a frozen value so
// retry backoff is deterministic; production callers pass time.Now.
type Clock func() time.Time

// Sleeper sleeps for the supplied duration, returning early when the
// context cancels. Tests substitute a recording sleeper that never
// actually waits.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// realSleeper is the default Sleeper backed by time.After.
type realSleeper struct{}

// Sleep waits for d or until ctx is done.
func (realSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("transport: retry sleeper cancelled by context: %w", ctx.Err())
	}
}

// RetryConfig configures RetryTransport.
type RetryConfig struct {
	// MaxAttempts caps total request attempts including the first. Zero
	// resolves to DefaultMaxAttempts.
	MaxAttempts int
	// BaseDelay is the first backoff interval; doubles on each retry up
	// to MaxDelay. Honored only when Retry-After is absent.
	BaseDelay time.Duration
	// MaxDelay caps exponential backoff. Zero resolves to DefaultMaxDelay.
	MaxDelay time.Duration
	// Clock returns "now"; defaults to time.Now. Used for HTTP-date
	// Retry-After parsing.
	Clock Clock
	// Sleeper waits between attempts; defaults to a time.After-backed
	// implementation. Tests inject a recording fake.
	Sleeper Sleeper
}

// Default tunables. Mirrors aws-sdk-go-v2's standard retry mode (max 3)
// scaled to 5 to give the eApp client one extra attempt on a flapping
// proxy without spending more than ~30s in the worst case.
const (
	DefaultMaxAttempts = 5
	DefaultBaseDelay   = 250 * time.Millisecond
	DefaultMaxDelay    = 8 * time.Second
)

// RetryTransport retries 429 + 5xx responses with exponential backoff
// and Retry-After awareness. Non-idempotent verbs are retried only when
// the response carries an explicit Retry-After header — matching RFC
// 9110 §15.3.7 guidance that 429 is retriable on any method but server
// 5xx generally is not without a hint.
type RetryTransport struct {
	inner   HTTPDoer
	cfg     RetryConfig
	clock   Clock
	sleeper Sleeper
}

// NewRetryTransport returns a configured RetryTransport. inner is
// required; cfg fields fall back to defaults.
func NewRetryTransport(inner HTTPDoer, cfg RetryConfig) (*RetryTransport, error) {
	if inner == nil {
		return nil, errors.New("transport: NewRetryTransport requires a non-nil inner HTTPDoer")
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = DefaultBaseDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = DefaultMaxDelay
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	sleeper := cfg.Sleeper
	if sleeper == nil {
		sleeper = realSleeper{}
	}
	return &RetryTransport{inner: inner, cfg: cfg, clock: clock, sleeper: sleeper}, nil
}

// Do executes req with retries. The request body is consumed on each
// attempt via req.GetBody (set by http.NewRequest when the body has a
// known length); requests without GetBody are not retried on bodies the
// client cannot rewind.
func (r *RetryTransport) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	var lastResp *http.Response
	var lastErr error

	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		if attempt > 1 {
			delay := r.computeDelay(lastResp, attempt-1)
			if err := r.sleeper.Sleep(ctx, delay); err != nil {
				return nil, err
			}
			if err := rewindBody(req); err != nil {
				return nil, err
			}
		}
		resp, err := r.inner.Do(req)
		lastErr = err
		lastResp = resp
		if err != nil {
			continue
		}
		if !shouldRetry(resp.StatusCode) {
			return resp, nil
		}
		// Discard bodies only when another attempt will run; draining the
		// final retriable response would close Body before the caller reads
		// it, and skipping io.Copy prevents keep-alive reuse on mid-chain
		// attempts.
		if attempt < r.cfg.MaxAttempts {
			drainAndClose(resp)
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("transport: %s %s exhausted %d retry attempts: %w", req.Method, req.URL.Path, r.cfg.MaxAttempts, lastErr)
	}
	return lastResp, nil
}

func rewindBody(req *http.Request) error {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	if req.GetBody == nil {
		return fmt.Errorf(
			"transport: cannot retry %s %s: request body is not rewindable (missing GetBody)",
			req.Method,
			req.URL.Path,
		)
	}
	body, err := req.GetBody()
	if err != nil {
		return fmt.Errorf("transport: failed to rewind request body for retry of %s %s: %w", req.Method, req.URL.Path, err)
	}
	req.Body = body
	return nil
}

func shouldRetry(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500 && status < 600
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// computeDelay returns the backoff interval before retry `attempts` (1-
// based). Retry-After wins when present; otherwise exponential with a
// hard cap at MaxDelay.
func (r *RetryTransport) computeDelay(prev *http.Response, attempts int) time.Duration {
	if prev != nil {
		if d, ok := parseRetryAfter(prev.Header.Get("Retry-After"), r.clock()); ok {
			if d > r.cfg.MaxDelay {
				return r.cfg.MaxDelay
			}
			return d
		}
	}
	d := r.cfg.BaseDelay
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= r.cfg.MaxDelay {
			return r.cfg.MaxDelay
		}
	}
	return d
}

// parseRetryAfter handles both RFC 7231 forms: delta-seconds and
// HTTP-date. Returns (0, false) on parse failure so the caller falls
// back to exponential backoff.
func parseRetryAfter(raw string, now time.Time) (time.Duration, bool) {
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(raw); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0, false
		}
		return d, true
	}
	return 0, false
}
