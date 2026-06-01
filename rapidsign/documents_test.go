package rapidsign

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/isa-sdk/sdk/rapidsign/internal"
)

// envelope wraps a response payload in the standard ADR-012 shape.
func envelope(payload any) []byte {
	body, err := json.Marshal(map[string]any{
		"object":     "document",
		"livemode":   false,
		"request_id": "req_test",
		"data":       payload,
	})
	if err != nil {
		panic(err)
	}
	return body
}

// errorBody writes a problem-details body with the supplied code.
func errorBody(status int, code, detail, param string) []byte {
	body, _ := json.Marshal(map[string]any{
		"type":   "https://api.example.com/errors/" + code,
		"title":  code,
		"status": status,
		"detail": detail,
		"code":   code,
		"param":  param,
	})
	return body
}

// fakeIDs returns deterministic identifiers so tests can assert on
// outbound headers.
type fakeIDs struct {
	counter atomic.Uint32
}

func newFakeIDs() *internal.IDSource {
	// internal.IDSource uses io.Reader for randomness; supply a
	// repeating 16-byte pattern via bytes.Reader on each call.
	return &internal.IDSource{Reader: &cyclicReader{seed: 0x42}}
}

// cyclicReader produces a deterministic stream by emitting a single
// byte that increments after every Read so successive UUIDs differ.
type cyclicReader struct{ seed byte }

func (r *cyclicReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.seed
		r.seed++
	}
	return len(p), nil
}

// newTestClient wires a Client at the supplied test server using a
// frozen clock and deterministic id source.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	frozen := time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC)
	c, err := New("isa_test_token", Options{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
		clock:      func() time.Time { return frozen },
		ids:        newFakeIDs(),
		sleeper:    &recordingSleeper{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// recordingSleeper records requested sleeps without actually waiting,
// so polling-loop tests run in milliseconds rather than minutes.
type recordingSleeper struct {
	sleeps []time.Duration
}

func (r *recordingSleeper) Sleep(stop <-chan struct{}, d time.Duration) bool {
	r.sleeps = append(r.sleeps, d)
	select {
	case <-stop:
		return false
	default:
		return true
	}
}

func TestDocumentsSend_IncludesExpectedHashes(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/documents/sig_abc/notify", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelope(map[string]any{"sign_id": "sig_abc", "status": "notified"}))
	})
	mux.HandleFunc("/v1/documents", func(w http.ResponseWriter, r *http.Request) {
		var body createDocumentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("create: decode body: %v", err)
			return
		}
		if got := body.ExpectedHashes["https://docs.example.com/a.pdf"]; got != "abc123" {
			t.Errorf("expected_hashes = %#v, want abc123 for a.pdf", body.ExpectedHashes)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(envelope(map[string]any{"sign_id": "sig_abc", "status": "pending"}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.Documents.Send(context.Background(), &SendRequest{
		Packet: []PdfSource{{
			URL:          "https://docs.example.com/a.pdf",
			ExpectedHash: "abc123",
		}},
		Recipient: Recipient{Email: "signer@example.com"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestDocumentsSend_HappyPath(t *testing.T) {
	t.Parallel()

	var createHits, notifyHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/documents/sig_abc/notify", func(w http.ResponseWriter, r *http.Request) {
		notifyHits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer isa_test_token" {
			t.Errorf("notify: Authorization = %q, want Bearer isa_test_token", got)
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("notify: missing Idempotency-Key header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelope(map[string]any{"sign_id": "sig_abc", "status": "notified"}))
	})
	mux.HandleFunc("/v1/documents", func(w http.ResponseWriter, r *http.Request) {
		createHits.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("create: method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer isa_test_token" {
			t.Errorf("create: Authorization = %q", got)
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("create: missing Idempotency-Key header")
		}
		var body createDocumentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("create: decode body: %v", err)
		}
		if body.SessionID == "" {
			t.Error("create: session_id was not minted")
		}
		if len(body.Packet) != 1 || body.Packet[0].URL != "https://docs.example.com/a.pdf" {
			t.Errorf("create: packet = %+v", body.Packet)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(envelope(map[string]any{
			"id":            "doc_123",
			"sign_id":       "sig_abc",
			"sign_url":      "https://rs.example.com/s/abc",
			"status":        "pending",
			"hashes":        map[string]string{"https://docs.example.com/a.pdf": "deadbeef"},
			"packet_stored": true,
		}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	env, err := c.Documents.Send(context.Background(), &SendRequest{
		Packet:    []PdfSource{{URL: "https://docs.example.com/a.pdf"}},
		Recipient: Recipient{Email: "signer@example.com", Name: "Signer"},
		LegalText: "I agree",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if env.SignID != "sig_abc" || env.ID != "doc_123" {
		t.Errorf("envelope = %+v", env)
	}
	if env.Status != DocumentStatusNotified {
		t.Errorf("status = %q, want notified", env.Status)
	}
	if createHits.Load() != 1 || notifyHits.Load() != 1 {
		t.Errorf("hits: create=%d notify=%d", createHits.Load(), notifyHits.Load())
	}
}

func TestDocumentsSend_ValidatesInputs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be reached")
		w.WriteHeader(500)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	cases := []struct {
		name string
		req  *SendRequest
	}{
		{"nil-request", nil},
		{"empty-packet", &SendRequest{Recipient: Recipient{Email: "a@b.com"}}},
		{"empty-email", &SendRequest{Packet: []PdfSource{{URL: "https://x"}}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := c.Documents.Send(context.Background(), tc.req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDocumentsSend_NotifyFailureReturnsEnvelope(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/documents/sig_abc/notify", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(errorBody(http.StatusBadGateway, "bad_gateway", "downstream emailer is down", ""))
	})
	mux.HandleFunc("/v1/documents", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(envelope(map[string]any{
			"id": "doc_1", "sign_id": "sig_abc", "status": "pending",
		}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(t, srv)
	c.doer = noRetryDoer(c.doer) // no need to retry 5xx in this test
	// Restore retries off — we want one shot per call. Build a minimal
	// transport that bypasses retry: easiest is to construct a client
	// with MaxAttempts=1. Skip optimisation: cap to 1 attempt.
	c2, err := New("isa_test_token", Options{
		BaseURL:     srv.URL,
		HTTPClient:  srv.Client(),
		MaxAttempts: 1,
		clock:       func() time.Time { return time.Unix(0, 0) },
		ids:         newFakeIDs(),
		sleeper:     &recordingSleeper{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	env, err := c2.Documents.Send(context.Background(), &SendRequest{
		Packet:    []PdfSource{{URL: "https://docs.example.com/a.pdf"}},
		Recipient: Recipient{Email: "x@y.com"},
	})
	if env == nil {
		t.Fatal("envelope must be returned even when notify fails after create")
	}
	if env.SignID != "sig_abc" {
		t.Errorf("envelope sign id = %q", env.SignID)
	}
	if err == nil {
		t.Fatal("expected wrapped notify error")
	}
}

// noRetryDoer is unused but kept as a placeholder hook; the test
// constructs its own client with MaxAttempts=1.
func noRetryDoer(d internal.Doer) internal.Doer { return d }

func TestDocumentsGet_HappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		sig := base64.StdEncoding.EncodeToString([]byte("PDFsig"))
		_, _ = w.Write(envelope(map[string]any{
			"sign_id":   "sig_abc",
			"signature": sig,
			"timestamp": 1715823600,
		}))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	got, err := c.Documents.Get(context.Background(), "sig_abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Signature) != "PDFsig" {
		t.Errorf("signature = %q", got.Signature)
	}
	if got.SignedAt.IsZero() {
		t.Error("SignedAt must be set")
	}
}

func TestDocumentsGet_RequiresSignID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("server should not be reached")
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Documents.Get(context.Background(), ""); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDocumentsDownload_DecompressesGzipBase64(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var gzBuf bytes.Buffer
		gw := gzip.NewWriter(&gzBuf)
		_, _ = gw.Write([]byte("%PDF-1.4 hello"))
		_ = gw.Close()
		b64 := base64.StdEncoding.EncodeToString(gzBuf.Bytes())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelope(map[string]any{
			"sign_id":         "sig_abc",
			"pdf_gzip_base64": b64,
			"compressed":      true,
			"size_bytes":      14,
		}))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	out, err := c.Documents.Download(context.Background(), "sig_abc")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(out) != "%PDF-1.4 hello" {
		t.Errorf("payload = %q", out)
	}
}

func TestDocumentsDownload_RejectsEmptyPayload(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelope(map[string]any{"sign_id": "sig_abc", "compressed": true}))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Documents.Download(context.Background(), "sig_abc"); err == nil {
		t.Fatal("expected error on empty payload")
	}
}

func TestDocumentsCancel_NotImplemented(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("server should not be reached")
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	err := c.Documents.Cancel(context.Background(), "sig_abc", &CancelRequest{Reason: "test"})
	var ni *NotImplementedError
	if !errors.As(err, &ni) {
		t.Fatalf("err = %T, want *NotImplementedError", err)
	}
}

// ----- error classification -----

func TestErrorClassification_FromProblemDetails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		code   string
		check  func(*testing.T, error)
	}{
		{
			"unauthorized", 401, "unauthorized",
			func(t *testing.T, err error) {
				var e *UnauthorizedError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
		{
			"token-expired", 401, "token_expired",
			func(t *testing.T, err error) {
				var e *TokenExpiredError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
		{
			"forbidden", 403, "forbidden",
			func(t *testing.T, err error) {
				var e *ForbiddenError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
		{
			"not-found", 404, "not_found",
			func(t *testing.T, err error) {
				var e *NotFoundError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
		{
			"conflict", 409, "conflict",
			func(t *testing.T, err error) {
				var e *ConflictError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
		{
			"validation", 400, "validation_error",
			func(t *testing.T, err error) {
				var e *ValidationError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
				if e.Field != "email" {
					t.Errorf("Field = %q", e.Field)
				}
			},
		},
		{
			"rate-limited", 429, "rate_limit_exceeded",
			func(t *testing.T, err error) {
				var e *RateLimitedError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
		{
			"service-unavailable", 503, "service_unavailable",
			func(t *testing.T, err error) {
				var e *ServiceUnavailableError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
		{
			"not-implemented", 501, "not_implemented",
			func(t *testing.T, err error) {
				var e *NotImplementedError
				if !errors.As(err, &e) {
					t.Fatalf("err = %T", err)
				}
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(tc.status)
				_, _ = w.Write(errorBody(tc.status, tc.code, "demo", "email"))
			}))
			defer srv.Close()
			c, err := New("isa_test_token", Options{
				BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
				clock: func() time.Time { return time.Unix(0, 0) }, ids: newFakeIDs(), sleeper: &recordingSleeper{},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, gotErr := c.Documents.Get(context.Background(), "sig_abc")
			if gotErr == nil {
				t.Fatal("expected error")
			}
			tc.check(t, gotErr)
		})
	}
}

func TestErrorClassification_RateLimitedRetryAfter(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write(errorBody(http.StatusTooManyRequests, "rate_limit_exceeded", "slow down", ""))
	}))
	defer srv.Close()
	c, err := New("isa_test_token", Options{
		BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
		clock: func() time.Time { return time.Unix(0, 0) }, ids: newFakeIDs(), sleeper: &recordingSleeper{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, gotErr := c.Documents.Get(context.Background(), "sig_abc")
	var rl *RateLimitedError
	if !errors.As(gotErr, &rl) {
		t.Fatalf("err = %T", gotErr)
	}
	if rl.Err.RetryAfter != 5*time.Second {
		t.Fatalf("RetryAfter = %s, want 5s", rl.Err.RetryAfter)
	}
}

func TestErrorClassification_UnknownCodeReturnsBaseError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write(errorBody(http.StatusTeapot, "mystery_code", "something odd", ""))
	}))
	defer srv.Close()
	c, err := New("isa_test_token", Options{
		BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
		clock: func() time.Time { return time.Unix(0, 0) }, ids: newFakeIDs(), sleeper: &recordingSleeper{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, gotErr := c.Documents.Get(context.Background(), "sig_abc")
	var base *Error
	if !errors.As(gotErr, &base) {
		t.Fatalf("err = %T, want *Error", gotErr)
	}
	if base.Code != ErrorCode("mystery_code") {
		t.Fatalf("Code = %q, want mystery_code", base.Code)
	}
	var nfe *NotFoundError
	if errors.As(gotErr, &nfe) {
		t.Fatal("unknown code must not be classified via HTTP status fallback")
	}
}

func TestErrorClassification_FallbackOnStatusOnly(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // no body, no Content-Type
	}))
	defer srv.Close()
	c, err := New("isa_test_token", Options{
		BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
		clock: func() time.Time { return time.Unix(0, 0) }, ids: newFakeIDs(), sleeper: &recordingSleeper{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, gotErr := c.Documents.Get(context.Background(), "sig_abc")
	var nfe *NotFoundError
	if !errors.As(gotErr, &nfe) {
		t.Fatalf("err = %T", gotErr)
	}
}

// ----- AwaitSignature -----

func TestAwaitSignature_PollsUntilSigned(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		sig := base64.StdEncoding.EncodeToString([]byte("done"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelope(map[string]any{"sign_id": "sig_abc", "signature": sig, "timestamp": 1715823600}))
	}))
	defer srv.Close()
	sleeper := &recordingSleeper{}
	c, err := New("isa_test_token", Options{
		BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
		clock: func() time.Time { return time.Unix(0, 0) }, ids: newFakeIDs(), sleeper: sleeper,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sig, err := c.Documents.AwaitSignature(context.Background(), "sig_abc", AwaitOpts{Timeout: time.Hour})
	if err != nil {
		t.Fatalf("AwaitSignature: %v", err)
	}
	if string(sig.Signature) != "done" {
		t.Errorf("signature = %q", sig.Signature)
	}
	if calls.Load() != 3 {
		t.Errorf("server hits = %d, want 3", calls.Load())
	}
	if len(sleeper.sleeps) < 2 {
		t.Errorf("expected at least 2 sleeps between polls, got %d", len(sleeper.sleeps))
	}
}

func TestAwaitSignature_RespectsContextCancel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())

	// A sleeper that cancels the context after first invocation, then
	// reports stop closed so the loop exits cleanly.
	cancelOnSleep := &cancellingSleeper{cancel: cancel}
	c, err := New("isa_test_token", Options{
		BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
		clock: func() time.Time { return time.Unix(0, 0) }, ids: newFakeIDs(), sleeper: cancelOnSleep,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Documents.AwaitSignature(ctx, "sig_abc", AwaitOpts{Timeout: time.Hour})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

type cancellingSleeper struct {
	cancel context.CancelFunc
	called bool
}

func (c *cancellingSleeper) Sleep(stop <-chan struct{}, _ time.Duration) bool {
	if !c.called {
		c.called = true
		c.cancel()
		// The Documents.AwaitSignature loop waits on ctx.Done via the
		// supplied stop channel, but our cancel races. Block briefly so
		// the cancel propagates, then return false to signal "stopped".
		<-stop
		return false
	}
	return false
}

func TestAwaitSignature_Timeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	clock := &mockClock{now: time.Unix(0, 0)}
	c, err := New("isa_test_token", Options{
		BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
		clock: clock.Now, ids: newFakeIDs(), sleeper: clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Documents.AwaitSignature(context.Background(), "sig_abc", AwaitOpts{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestAwaitSignature_BlockedGetRespectsTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	clock := &mockClock{now: time.Unix(0, 0)}
	c, err := New("isa_test_token", Options{
		BaseURL: srv.URL, HTTPClient: srv.Client(), MaxAttempts: 1,
		clock: clock.Now, ids: newFakeIDs(), sleeper: clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Documents.AwaitSignature(context.Background(), "sig_abc", AwaitOpts{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

// mockClock both ticks the clock forward on every Sleep call and acts
// as a Sleeper, so the AwaitSignature loop's deadline math advances.
type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time { return m.now }

func (m *mockClock) Sleep(_ <-chan struct{}, d time.Duration) bool {
	m.now = m.now.Add(d)
	return true
}

// ----- internal helpers -----

func TestDecodeGzippedBase64_RoundTrip(t *testing.T) {
	t.Parallel()
	src := []byte("hello rapidsign")
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(src); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(gzBuf.Bytes())
	got, err := internal.DecodeGzippedBase64(b64)
	if err != nil {
		t.Fatalf("DecodeGzippedBase64: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Errorf("round-trip mismatch: got %q want %q", got, src)
	}
}

func TestDecodeBase64Signature_AcceptsMultipleEncodings(t *testing.T) {
	t.Parallel()
	src := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}
	encs := []string{
		base64.StdEncoding.EncodeToString(src),
		base64.URLEncoding.EncodeToString(src),
	}
	for _, enc := range encs {
		got, err := decodeBase64Signature(enc)
		if err != nil {
			t.Fatalf("decode %q: %v", enc, err)
		}
		if !bytes.Equal(got, src) {
			t.Errorf("decode mismatch: got %v want %v", got, src)
		}
	}
}

// ----- IdempotencyKey override -----

func TestDocumentsSend_HonorsCallerIdempotencyKey(t *testing.T) {
	t.Parallel()
	var sawKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sawKey == "" {
			sawKey = r.Header.Get("Idempotency-Key")
		}
		if r.URL.Path == "/v1/documents" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(envelope(map[string]any{"id": "doc_1", "sign_id": "sig_abc", "status": "pending"}))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(envelope(map[string]any{"sign_id": "sig_abc", "status": "notified"}))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Documents.Send(context.Background(), &SendRequest{
		Packet:    []PdfSource{{URL: "https://docs.example.com/a.pdf"}},
		Recipient: Recipient{Email: "s@e.com"},
	}, SendOptions{IdempotencyKey: "caller-supplied-key-123"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sawKey != "caller-supplied-key-123" {
		t.Errorf("Idempotency-Key on create = %q, want caller-supplied-key-123", sawKey)
	}
}

// drain helper used by sub-tests; kept for symmetry with the real
// transport draining behavior.
var _ = io.Discard
