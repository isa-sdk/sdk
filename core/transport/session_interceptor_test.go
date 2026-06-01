package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/isa-sdk/sdk/core/session"
)

// recordingMockDoer counts bootstrap vs product calls. It returns a
// canned bootstrap response for POST /v1/sessions and a 200 with the
// signed headers echoed for everything else.
type recordingMockDoer struct {
	bootstrapHits int32
	productHits   int32
	// expireFirstProduct causes the first product response to return
	// 401 session_expired so the retry path can be exercised.
	expireFirstProduct atomic.Bool
	revokeFirstProduct atomic.Bool
}

func (r *recordingMockDoer) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/v1/sessions" {
		atomic.AddInt32(&r.bootstrapHits, 1)
		// Slow the bootstrap call slightly so concurrent product
		// requests collapse onto the single-flight result.
		time.Sleep(20 * time.Millisecond)
		body := map[string]any{
			"object": "session",
			"data": map[string]string{
				"sessionId":     "sess_test_01HZK2N5GQR9T8X4B6FJW3Y1AS",
				"sessionSecret": "secret_test_4fjK2nQ7mX1aB8sR9pZ3",
				"expiresAt":     time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
			},
		}
		raw, _ := json.Marshal(body)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}
	atomic.AddInt32(&r.productHits, 1)
	if r.expireFirstProduct.Swap(false) {
		raw, _ := json.Marshal(map[string]string{"code": "session_expired", "type": "about:blank"})
		return &http.Response{
			StatusCode: 401,
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		}, nil
	}
	if r.revokeFirstProduct.Swap(false) {
		raw, _ := json.Marshal(map[string]string{"code": "session_revoked", "type": "about:blank"})
		return &http.Response{
			StatusCode: 401,
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		}, nil
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func newTestStore(t *testing.T, doer session.HTTPDoer) *session.Store {
	t.Helper()
	store, storeErr := session.NewStore(doer, session.SystemClock{}, "https://api.example.test", session.ExchangeInput{
		Keycode:    "SDV-HWH-WDD",
		Email:      "john.doe@acme-agency.com",
		LicenseKey: "zyins_test_4fjK2nQ7mX1aB8sR9pZ3",
		DeviceID:   "device-abc-123",
	})
	if storeErr != nil {
		t.Fatalf("NewStore: %v", storeErr)
	}
	return store
}

// TestSessionInterceptor_ConcurrentProductCalls_TriggerExactlyOneBootstrap
// fires 10 concurrent product calls from a cold-start interceptor and
// asserts the underlying mock saw exactly one POST /v1/sessions. This
// is the single-flight invariant the task contract requires.
func TestSessionInterceptor_ConcurrentProductCalls_TriggerExactlyOneBootstrap(t *testing.T) {
	mock := &recordingMockDoer{}
	store := newTestStore(t, mock)
	interceptor, ctorErr := NewSessionInterceptor(store, mock)
	if ctorErr != nil {
		t.Fatalf("NewSessionInterceptor: %v", ctorErr)
	}
	const concurrency = 10
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "https://api.example.test/v1/prequalify", strings.NewReader(`{"x":1}`))
			resp, doErr := interceptor.Do(req)
			if doErr != nil {
				t.Errorf("Do: %v", doErr)
				return
			}
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&mock.bootstrapHits); got != 1 {
		t.Fatalf("expected exactly 1 bootstrap under single-flight, got %d", got)
	}
	if got := atomic.LoadInt32(&mock.productHits); got != concurrency {
		t.Fatalf("expected %d product hits, got %d", concurrency, got)
	}
}

// TestSessionInterceptor_RetryOn401SessionExpired verifies that a 401
// with code=session_expired triggers Invalidate + Bootstrap + one
// replay.
func TestSessionInterceptor_RetryOn401SessionExpired(t *testing.T) {
	mock := &recordingMockDoer{}
	mock.expireFirstProduct.Store(true)
	store := newTestStore(t, mock)
	interceptor, ctorErr := NewSessionInterceptor(store, mock)
	if ctorErr != nil {
		t.Fatalf("NewSessionInterceptor: %v", ctorErr)
	}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "https://api.example.test/v1/prequalify", strings.NewReader(`{"x":1}`))
	resp, doErr := interceptor.Do(req)
	if doErr != nil {
		t.Fatalf("Do: %v", doErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected final 200 after retry, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&mock.bootstrapHits); got != 2 {
		t.Fatalf("expected 2 bootstraps (cold + retry), got %d", got)
	}
	if got := atomic.LoadInt32(&mock.productHits); got != 2 {
		t.Fatalf("expected 2 product hits (401 + replay), got %d", got)
	}
}

func TestSessionInterceptor_InvalidatesWithoutRetryOn401SessionRevoked(t *testing.T) {
	mock := &recordingMockDoer{}
	mock.revokeFirstProduct.Store(true)
	store := newTestStore(t, mock)
	interceptor, ctorErr := NewSessionInterceptor(store, mock)
	if ctorErr != nil {
		t.Fatalf("NewSessionInterceptor: %v", ctorErr)
	}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "https://api.example.test/v1/prequalify", strings.NewReader(`{"x":1}`))
	resp, doErr := interceptor.Do(req)
	if doErr != nil {
		t.Fatalf("Do: %v", doErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected revoked response to be returned, got %d", resp.StatusCode)
	}
	if store.CurrentSecret() != nil {
		t.Fatalf("expected revoked session to be invalidated")
	}
	if got := atomic.LoadInt32(&mock.bootstrapHits); got != 1 {
		t.Fatalf("expected 1 bootstrap before revoked response, got %d", got)
	}
	if got := atomic.LoadInt32(&mock.productHits); got != 1 {
		t.Fatalf("expected no retry for revoked session, got %d product hits", got)
	}
}

func TestSessionProblemCode_PreservesLargeBody(t *testing.T) {
	body := `{"code":"session_expired"}` + strings.Repeat(" ", maxProblemBodyBytes*2)
	resp := &http.Response{
		StatusCode: 401,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
	}
	if got := sessionProblemCode(resp); got != sessionExpiredCode {
		t.Fatalf("sessionProblemCode = %q, want %q", got, sessionExpiredCode)
	}
	restored, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read restored body: %v", readErr)
	}
	if string(restored) != body {
		t.Fatalf("restored body changed: got %d bytes, want %d", len(restored), len(body))
	}
}

// TestSessionInterceptor_OnActivity_ProactiveRefresh exercises the
// consumer-facing OnActivity hook.
func TestSessionInterceptor_OnActivity_ProactiveRefresh(t *testing.T) {
	mock := &recordingMockDoer{}
	store := newTestStore(t, mock)
	if onErr := store.OnActivity(context.Background()); onErr != nil {
		t.Fatalf("OnActivity cold: %v", onErr)
	}
	if got := atomic.LoadInt32(&mock.bootstrapHits); got != 1 {
		t.Fatalf("expected 1 bootstrap from OnActivity cold-start, got %d", got)
	}
	store.Invalidate()
	if onErr := store.OnActivity(context.Background()); onErr != nil {
		t.Fatalf("OnActivity after invalidation: %v", onErr)
	}
	if got := atomic.LoadInt32(&mock.bootstrapHits); got != 2 {
		t.Fatalf("expected second bootstrap after invalidation, got %d", got)
	}
}

func TestSessionStore_BootstrapCachesAfterCallerCancellation(t *testing.T) {
	mock := &recordingMockDoer{}
	store := newTestStore(t, mock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, bsErr := store.Bootstrap(ctx); bsErr == nil {
		t.Fatalf("expected canceled bootstrap caller to receive an error")
	}

	deadline := time.After(200 * time.Millisecond)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for store.CurrentSecret() == nil {
		select {
		case <-deadline:
			t.Fatalf("bootstrap result was not cached after caller cancellation")
		case <-tick.C:
		}
	}
	if got := atomic.LoadInt32(&mock.bootstrapHits); got != 1 {
		t.Fatalf("expected canceled caller to trigger one bootstrap, got %d", got)
	}
}
