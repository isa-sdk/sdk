package zyins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/sync/errgroup"
)

func TestPrequalify_RunWithRawResponse_ReturnsEnvelopeAndRaw(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_01HZK2N5GQR9T8X4B6FJW3Y1AS")
		_, _ = w.Write([]byte(`{
			"data": {
				"plans": [{"brand":"colonial-penn","tier":"preferred","monthly_premium_cents":4995,"face_value_cents":1000000,"product_token":"colonial-penn.final-expense"}]
			},
			"request_id": "req_01HZK2N5GQR9T8X4B6FJW3Y1AS",
			"idempotency_key": "k-123",
			"livemode": true,
			"retry_attempts": 0
		}`))
		_ = r
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	cov, _ := NewFaceValueCoverage(100_000)
	sel, _ := NewProductSelection("colonial-penn.final-expense")
	env, raw, err := c.Prequalify.RunWithRawResponse(context.Background(), &PrequalifyInput{
		Applicant: validApplicant(t), Coverage: cov, Products: sel,
	})
	if err != nil {
		t.Fatalf("RunWithRawResponse: %v", err)
	}
	if raw == nil || raw.Status != http.StatusOK {
		t.Fatalf("raw response missing or wrong status: %+v", raw)
	}
	if raw.Header.Get("X-Request-Id") == "" {
		t.Errorf("raw headers must echo X-Request-Id")
	}
	if env.RequestID != "req_01HZK2N5GQR9T8X4B6FJW3Y1AS" {
		t.Errorf("Envelope.RequestID = %q", env.RequestID)
	}
	if env.IdempotencyKey != "k-123" {
		t.Errorf("Envelope.IdempotencyKey = %q", env.IdempotencyKey)
	}
	if !env.Livemode {
		t.Errorf("Envelope.Livemode = false")
	}
	if env.Data == nil || len(env.Data.Plans) != 1 {
		t.Errorf("Envelope.Data missing or wrong: %+v", env.Data)
	}
}

func TestPrequalify_RunWithRawResponse_FallsBackToOutboundIdempotencyKey(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server omits idempotency_key in the envelope; SDK must fall
		// back to echoing the outbound header value.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"plans":[]},"request_id":"req_x"}`))
		_ = r
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	cov, _ := NewFaceValueCoverage(100_000)
	sel, _ := NewProductSelection("x.y")
	env, _, err := c.Prequalify.RunWithRawResponse(context.Background(), &PrequalifyInput{
		Applicant: validApplicant(t), Coverage: cov, Products: sel,
	}, WithIdempotencyKey("custom-key-42"))
	if err != nil {
		t.Fatalf("RunWithRawResponse: %v", err)
	}
	if env.IdempotencyKey != "custom-key-42" {
		t.Errorf("Envelope.IdempotencyKey = %q, want custom-key-42", env.IdempotencyKey)
	}
}

func TestIdempotencyConflictError_ParsedFromProblemDetails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Idempotency-Key", "key-already-seen")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"idempotency_conflict","detail":"key reused with different body","first_seen_at":"2026-05-14T14:32:01Z"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	cov, _ := NewFaceValueCoverage(50_000)
	sel, _ := NewProductSelection("x.y")
	_, err := c.Prequalify.Run(context.Background(), &PrequalifyInput{
		Applicant: validApplicant(t), Coverage: cov, Products: sel,
	})
	var ice *IdempotencyConflictError
	if !errors.As(err, &ice) {
		t.Fatalf("expected *IdempotencyConflictError; got %T: %v", err, err)
	}
	if ice.Key != "key-already-seen" {
		t.Errorf("Key = %q", ice.Key)
	}
	if ice.FirstSeenAt.IsZero() {
		t.Errorf("FirstSeenAt should be parsed; got zero")
	}
	if ice.IsaCode() != ErrorCodeIdempotencyConflict {
		t.Errorf("IsaCode = %q", ice.IsaCode())
	}
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("errors.Is(err, ErrIdempotencyConflict) should be true")
	}
}

func TestIdempotencyConflictError_FallbackFromConflictStatusOnly(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Idempotency-Key", "k1")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(""))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	cov, _ := NewFaceValueCoverage(50_000)
	sel, _ := NewProductSelection("x.y")
	_, err := c.Prequalify.Run(context.Background(), &PrequalifyInput{
		Applicant: validApplicant(t), Coverage: cov, Products: sel,
	})
	var ice *IdempotencyConflictError
	if !errors.As(err, &ice) {
		t.Fatalf("expected *IdempotencyConflictError; got %T", err)
	}
}

// TestClient_ConcurrentRequestsHaveDistinctIdempotencyKeys launches 100
// concurrent prequalify calls and asserts that every Idempotency-Key
// the server receives is unique. This is the concurrency-safety
// guarantee documented in the README; regressions here would manifest
// as collision-driven server-side de-dup behavior in production.
func TestClient_ConcurrentRequestsHaveDistinctIdempotencyKeys(t *testing.T) {
	t.Parallel()
	const n = 100
	var (
		mu   sync.Mutex
		keys = make(map[string]struct{}, n)
		hits int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		mu.Lock()
		keys[key] = struct{}{}
		mu.Unlock()
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fmt.Appendf(nil, `{"data":{"plans":[]},"request_id":%q}`, key))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	cov, _ := NewFaceValueCoverage(100_000)
	sel, _ := NewProductSelection("x.y")

	var g errgroup.Group
	for range n {
		g.Go(func() error {
			_, err := c.Prequalify.Run(context.Background(), &PrequalifyInput{
				Applicant: validApplicant(t), Coverage: cov, Products: sel,
			})
			return err
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent Run failed: %v", err)
	}
	if got := atomic.LoadInt64(&hits); got != n {
		t.Fatalf("server hits = %d, want %d", got, n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != n {
		t.Errorf("expected %d distinct keys; got %d", n, len(keys))
	}
	if _, present := keys[""]; present {
		t.Errorf("empty Idempotency-Key observed")
	}
}

// TestClient_ConcurrentRequestsHaveDistinctRequestIDs mirrors the
// idempotency-key test but asserts on the server-returned RequestID
// instead. The conformance corpus calls this out as a launch criterion
// (SDK_DESIGN.md §14.3 last bullet).
func TestClient_ConcurrentRequestsHaveDistinctRequestIDs(t *testing.T) {
	t.Parallel()
	const n = 100
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the inbound Idempotency-Key as the request_id so we
		// transitively assert end-to-end distinctness.
		key := r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fmt.Appendf(nil, `{"data":{"plans":[],"request_id":%q}}`, key))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	cov, _ := NewFaceValueCoverage(100_000)
	sel, _ := NewProductSelection("x.y")

	results := make([]string, n)
	var g errgroup.Group
	for i := range n {
		g.Go(func() error {
			res, err := c.Prequalify.Run(context.Background(), &PrequalifyInput{
				Applicant: validApplicant(t), Coverage: cov, Products: sel,
			})
			if err != nil {
				return err
			}
			results[i] = res.RequestID
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent Run failed: %v", err)
	}
	seen := make(map[string]struct{}, n)
	for _, id := range results {
		if id == "" {
			t.Errorf("empty RequestID in concurrent batch")
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Errorf("expected %d distinct RequestIDs; got %d", n, len(seen))
	}
}
