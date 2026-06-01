package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// scriptedDoer returns a sequence of pre-built responses (and errors).
// Each Do call consumes the next entry; running out is a test bug.
type scriptedDoer struct {
	t       *testing.T
	steps   []scriptedStep
	idx     int
	seenReq []*http.Request
}

type scriptedStep struct {
	status     int
	retryAfter string
	body       string
	err        error
}

func (s *scriptedDoer) Do(req *http.Request) (*http.Response, error) {
	s.t.Helper()
	if s.idx >= len(s.steps) {
		s.t.Fatalf("scriptedDoer: out of scripted steps after %d calls", s.idx)
	}
	step := s.steps[s.idx]
	s.idx++
	s.seenReq = append(s.seenReq, req)
	if step.err != nil {
		return nil, step.err
	}
	resp := &http.Response{
		StatusCode: step.status,
		Body:       io.NopCloser(strings.NewReader(step.body)),
		Header:     http.Header{},
	}
	if step.retryAfter != "" {
		resp.Header.Set("Retry-After", step.retryAfter)
	}
	return resp, nil
}

// recordedSleeper does not actually sleep; it records the durations so
// tests assert the backoff schedule.
type recordedSleeper struct {
	calls []time.Duration
}

func (r *recordedSleeper) Sleep(_ context.Context, d time.Duration) error {
	r.calls = append(r.calls, d)
	return nil
}

// Test tunables. Kept small so exponential-backoff assertions don't
// pad runtime, but with a MaxDelay that comfortably bounds the HTTP-
// date retry-after fixture (3s) without clipping.
const (
	testMaxAttempts = 4
	testBaseDelay   = 10 * time.Millisecond
	testMaxDelay    = 10 * time.Second
)

func newRetryConfigForTest(sleeper Sleeper, clock Clock) RetryConfig {
	return RetryConfig{
		MaxAttempts: testMaxAttempts,
		BaseDelay:   testBaseDelay,
		MaxDelay:    testMaxDelay,
		Clock:       clock,
		Sleeper:     sleeper,
	}
}

func TestRetryTransport_HappyPath_NoRetry(t *testing.T) {
	doer := &scriptedDoer{t: t, steps: []scriptedStep{{status: 200, body: "ok"}}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	resp, err := rt.Do(httptest.NewRequest("GET", "/v1/x", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if doer.idx != 1 {
		t.Fatalf("expected 1 attempt, got %d", doer.idx)
	}
	if len(sleeper.calls) != 0 {
		t.Fatalf("expected zero sleeps, got %v", sleeper.calls)
	}
}

func TestRetryTransport_429_RetriesUntilSuccess(t *testing.T) {
	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{status: 429, retryAfter: "1"},
		{status: 429, retryAfter: "2"},
		{status: 200, body: "ok"},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	resp, err := rt.Do(httptest.NewRequest("GET", "/v1/x", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("final status = %d, want 200", resp.StatusCode)
	}
	if doer.idx != 3 {
		t.Fatalf("expected 3 attempts, got %d", doer.idx)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second}
	if len(sleeper.calls) != len(want) {
		t.Fatalf("sleep count = %d, want %d (%v)", len(sleeper.calls), len(want), sleeper.calls)
	}
	for i := range want {
		if sleeper.calls[i] != want[i] {
			t.Fatalf("sleep[%d] = %v, want %v", i, sleeper.calls[i], want[i])
		}
	}
}

func TestRetryTransport_TransportErrorAfterRetryable_ClearsRetryAfterForNextDelay(t *testing.T) {
	netErr := errors.New("connection reset")
	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{status: 429, retryAfter: "1"},
		{err: netErr},
		{status: 200, body: "ok"},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	resp, err := rt.Do(httptest.NewRequest("GET", "/v1/x", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("final status = %d, want 200", resp.StatusCode)
	}
	want := []time.Duration{1 * time.Second, 2 * testBaseDelay}
	if len(sleeper.calls) != len(want) {
		t.Fatalf("sleep count = %d, want %d (%v)", len(sleeper.calls), len(want), sleeper.calls)
	}
	for i := range want {
		if sleeper.calls[i] != want[i] {
			t.Fatalf("sleep[%d] = %v, want %v", i, sleeper.calls[i], want[i])
		}
	}
}

func TestRetryTransport_5xx_FallsBackToExponentialWhenNoRetryAfter(t *testing.T) {
	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{status: 503},
		{status: 503},
		{status: 200, body: "ok"},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if _, err := rt.Do(httptest.NewRequest("GET", "/v1/x", nil)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	// First retry: BaseDelay (10ms). Second retry: 10ms * 2 = 20ms.
	if len(sleeper.calls) != 2 || sleeper.calls[0] != 10*time.Millisecond || sleeper.calls[1] != 20*time.Millisecond {
		t.Fatalf("exponential schedule = %v, want [10ms 20ms]", sleeper.calls)
	}
}

func TestRetryTransport_HTTPDateRetryAfter_HonoredVsClock(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{status: 429, retryAfter: fixed.Add(3 * time.Second).UTC().Format(http.TimeFormat)},
		{status: 200, body: "ok"},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, func() time.Time { return fixed }))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if _, err := rt.Do(httptest.NewRequest("GET", "/v1/x", nil)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(sleeper.calls) != 1 {
		t.Fatalf("want exactly one sleep, got %v", sleeper.calls)
	}
	// HTTP-date arithmetic rounds to whole seconds.
	if sleeper.calls[0] < 2*time.Second || sleeper.calls[0] > 4*time.Second {
		t.Fatalf("sleep = %v, want ~3s", sleeper.calls[0])
	}
}

func TestRetryTransport_ExhaustsAttemptsAndReturnsLastError(t *testing.T) {
	netErr := errors.New("connection reset")
	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{err: netErr}, {err: netErr}, {err: netErr}, {err: netErr},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	_, err = rt.Do(httptest.NewRequest("GET", "/v1/x", nil))
	if !errors.Is(err, netErr) {
		t.Fatalf("expected wrapped netErr, got %v", err)
	}
	if doer.idx != 4 {
		t.Fatalf("expected 4 attempts (MaxAttempts), got %d", doer.idx)
	}
}

func TestRetryTransport_NegativeRetryAfter_FallsThroughToExponential(t *testing.T) {
	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{status: 503, retryAfter: "-5"},
		{status: 200, body: "ok"},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if _, err := rt.Do(httptest.NewRequest("GET", "/v1/x", nil)); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(sleeper.calls) != 1 || sleeper.calls[0] != 10*time.Millisecond {
		t.Fatalf("expected fallback to BaseDelay (10ms), got %v", sleeper.calls)
	}
}

func TestRetryTransport_AllRetriable_Exhausted_ReturnsLastResponseWithReadableBody(t *testing.T) {
	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{status: 429, retryAfter: "1"},
		{status: 429, retryAfter: "1"},
		{status: 429, retryAfter: "1"},
		{status: 429, body: "rate limited"},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	resp, err := rt.Do(httptest.NewRequest("GET", "/v1/x", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("final status = %d, want 429", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "rate limited" {
		t.Fatalf("body = %q, want rate limited", string(body))
	}
}

func TestRetryTransport_NonRewindableBody_ReturnsErrorBeforeSecondAttempt(t *testing.T) {
	u, err := url.Parse("http://example.com/v1/x")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	req := (&http.Request{
		Method: "POST",
		URL:    u,
		Header: make(http.Header),
		Body:   io.NopCloser(strings.NewReader("payload")),
	}).WithContext(context.Background())
	req.GetBody = nil

	doer := &scriptedDoer{t: t, steps: []scriptedStep{
		{status: 429, retryAfter: "1"},
		{status: 200, body: "ok"},
	}}
	sleeper := &recordedSleeper{}
	rt, err := NewRetryTransport(doer, newRetryConfigForTest(sleeper, time.Now))
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	_, err = rt.Do(req)
	if err == nil {
		t.Fatalf("expected error when body cannot rewind for retry")
	}
	if !strings.Contains(err.Error(), "not rewindable") {
		t.Fatalf("err = %v", err)
	}
	if doer.idx != 1 {
		t.Fatalf("expected inner Do not to run after rewind failure, got %d calls", doer.idx)
	}
}

func TestParseRetryAfter_RejectsGarbage(t *testing.T) {
	if _, ok := parseRetryAfter("not-a-number-or-date", time.Now()); ok {
		t.Fatalf("expected garbage Retry-After to be rejected")
	}
}
