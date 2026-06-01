package zyins

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/isa-sdk/sdk/zyins/cases"
)

// matchTopFakeDoer counts requests so tests can assert cache hits / misses.
// Each call returns a fresh body reader so concurrent calls don't race on
// a drained io.Reader.
type matchTopFakeDoer struct {
	body   string
	status int
	calls  atomic.Int32
}

func (f *matchTopFakeDoer) Do(_ *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{},
	}, nil
}

const matchTopBundleJSON = `{
  "data": {
    "version": "2026-05-29",
    "medications_by_condition": {
      "HBP": ["LISINOPRIL", "AMLODIPINE"],
      "DIABETES": ["METFORMIN"]
    },
    "frequency_graphs": {
      "use_map": {
        "HBP": {"LISINOPRIL": 100, "AMLODIPINE": 50},
        "DIABETES": {"METFORMIN": 200}
      }
    },
    "datasets": {
      "medications": {
        "version": "2026-05-29",
        "item_count": 3,
        "items": [
          {"id": "LISINOPRIL", "name": "Lisinopril"},
          {"id": "AMLODIPINE", "name": "Amlodipine"},
          {"id": "METFORMIN", "name": "Metformin"}
        ]
      },
      "conditions": {
        "version": "2026-05-29",
        "item_count": 2,
        "items": [
          {"id": "HBP", "name": "High Blood Pressure"},
          {"id": "DIABETES", "name": "Diabetes"}
        ]
      }
    }
  }
}`

func newMatchTopClient(t *testing.T) (*Client, *matchTopFakeDoer) {
	t.Helper()
	doer := &matchTopFakeDoer{body: matchTopBundleJSON, status: http.StatusOK}
	c, err := NewClient(WithToken("isa_test_aaaaaaaaaaaaaaaaaaaa"), WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.doer = doer
	return c, doer
}

func TestClient_Medications_Match_KnownText(t *testing.T) {
	c, _ := newMatchTopClient(t)
	cases := []struct {
		name   string
		text   string
		wantID string
	}{
		{"exact-id", "LISINOPRIL", "LISINOPRIL"},
		{"display-name", "Lisinopril", "LISINOPRIL"},
		{"lowercased", "lisinopril", "LISINOPRIL"},
		{"with-spaces", "  Lisinopril  ", "LISINOPRIL"},
		{"metformin", "Metformin", "METFORMIN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Medications().Match(context.Background(), tc.text)
			if err != nil {
				t.Fatalf("Match: %v", err)
			}
			if got == nil {
				t.Fatal("Match returned nil for known text")
			}
			if got.ID() != tc.wantID {
				t.Errorf("ID = %q, want %q", got.ID(), tc.wantID)
			}
			if !got.IsKnown() {
				t.Error("IsKnown = false, want true")
			}
		})
	}
}

func TestClient_Medications_Match_UnknownTextReturnsNil(t *testing.T) {
	c, _ := newMatchTopClient(t)
	got, err := c.Medications().Match(context.Background(), "totally-unknown-drug")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got != nil {
		t.Errorf("Match for unknown text = %v, want nil", got)
	}
}

func TestClient_Conditions_Match_KnownText(t *testing.T) {
	c, _ := newMatchTopClient(t)
	// "hbp" normalizes to "HBP" which is the catalog id; the matcher
	// keys on the normalized id, not on display name.
	got, err := c.Conditions().Match(context.Background(), "hbp")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got == nil {
		t.Fatal("Match returned nil for known condition")
	}
	if got.ID() != "HBP" {
		t.Errorf("ID = %q, want HBP", got.ID())
	}
	if got.Name() != "High Blood Pressure" {
		t.Errorf("Name = %q, want %q", got.Name(), "High Blood Pressure")
	}
}

func TestClient_Conditions_Match_UnknownReturnsNil(t *testing.T) {
	c, _ := newMatchTopClient(t)
	got, err := c.Conditions().Match(context.Background(), "qwerty zxcv")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got != nil {
		t.Errorf("Match for unknown text = %v, want nil", got)
	}
}

func TestClient_Concepts_Match_PreservesUnknownHandle(t *testing.T) {
	c, _ := newMatchTopClient(t)
	got, err := c.Concepts().Match(context.Background(), "qwerty zxcv")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got == nil {
		t.Fatal("Concepts.Match returned nil — should return unknown handle")
	}
	if got.IsKnown() {
		t.Error("IsKnown = true, want false")
	}
	if got.InputText() != "qwerty zxcv" {
		t.Errorf("InputText = %q, want %q", got.InputText(), "qwerty zxcv")
	}
}

func TestClient_Concepts_Match_ResolvesConditionFirst(t *testing.T) {
	c, _ := newMatchTopClient(t)
	got, err := c.Concepts().Match(context.Background(), "HBP")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !got.IsKnown() {
		t.Fatal("expected known concept")
	}
	if got.ID() != "HBP" {
		t.Errorf("ID = %q, want HBP", got.ID())
	}
}

func TestClient_Match_CachesBundleAcrossCalls(t *testing.T) {
	c, doer := newMatchTopClient(t)
	ctx := context.Background()
	for i := range 5 {
		if _, err := c.Medications().Match(ctx, "Lisinopril"); err != nil {
			t.Fatalf("Match[%d]: %v", i, err)
		}
	}
	if _, err := c.Conditions().Match(ctx, "HBP"); err != nil {
		t.Fatalf("Conditions.Match: %v", err)
	}
	if _, err := c.Concepts().Match(ctx, "HBP"); err != nil {
		t.Fatalf("Concepts.Match: %v", err)
	}
	if got := doer.calls.Load(); got != 1 {
		t.Errorf("fetch calls = %d, want 1 (cache should serve all but first)", got)
	}
}

func TestClient_RefreshReferenceIndex_InvalidatesCache(t *testing.T) {
	c, doer := newMatchTopClient(t)
	ctx := context.Background()
	if _, err := c.Medications().Match(ctx, "Lisinopril"); err != nil {
		t.Fatalf("Match: %v", err)
	}
	if got := doer.calls.Load(); got != 1 {
		t.Fatalf("after first Match: calls = %d, want 1", got)
	}
	c.RefreshReferenceIndex()
	if _, err := c.Medications().Match(ctx, "Lisinopril"); err != nil {
		t.Fatalf("Match after refresh: %v", err)
	}
	if got := doer.calls.Load(); got != 2 {
		t.Errorf("after Refresh + Match: calls = %d, want 2", got)
	}
}

func TestClient_Match_ConcurrentCallsCoalesce(t *testing.T) {
	c, doer := newMatchTopClient(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, _ = c.Medications().Match(ctx, "Lisinopril")
		}()
	}
	wg.Wait()
	if got := doer.calls.Load(); got != 1 {
		t.Errorf("concurrent first Match: fetch calls = %d, want 1 (coalesce)", got)
	}
}

// matchTopBlockingDoer models a real HTTP transport: it holds the
// request open until released, and aborts with the request context's
// error if that context is cancelled first (exactly what
// net/http.Client.Do does). This lets a test force a genuine overlap
// window — caller A's fetch is in flight (entered closed, blocked on
// release) while caller B coalesces onto it — and then prove the shared
// fetch survives caller A cancelling its context.
type matchTopBlockingDoer struct {
	body    string
	entered chan struct{} // closed when Do is first invoked (fetch in flight)
	release chan struct{} // close to let the blocked fetch complete
	once    sync.Once
	calls   atomic.Int32
}

func newMatchTopBlockingDoer(body string) *matchTopBlockingDoer {
	return &matchTopBlockingDoer{
		body:    body,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (d *matchTopBlockingDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	d.once.Do(func() { close(d.entered) })
	select {
	case <-d.release:
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(d.body)),
			Header:     http.Header{},
		}, nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

// matchTopErrDoer always fails — exercises the bundle-fetch error path.
type matchTopErrDoer struct{}

func (matchTopErrDoer) Do(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"internal","message":"boom"}}`)),
		Header:     http.Header{},
	}, nil
}

func TestClient_Match_BundleFetchErrorSurfaces(t *testing.T) {
	c, err := NewClient(WithToken("isa_test_aaaaaaaaaaaaaaaaaaaa"), WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.doer = matchTopErrDoer{}
	_, err = c.Medications().Match(context.Background(), "Lisinopril")
	if err == nil {
		t.Fatal("expected bundle-fetch error")
	}
	if !strings.Contains(err.Error(), "reference index fetch") {
		t.Errorf("error %q missing 'reference index fetch' wrap", err)
	}
}

// -----------------------------------------------------------------------------
// Cases.Save / Cases.Recall round-trip.
// -----------------------------------------------------------------------------

func TestCasesService_Save_Recall_RoundTrip(t *testing.T) {
	mock := newMockCaseStorage()
	c := newCaseStorageTestClient(t, WithCaseStorage(mock))

	put, err := c.Cases.Save(context.Background(), cases.CaseRecord{
		Product: "zyins",
		Body:    []byte("encrypted-payload"),
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if put.RecallToken == "" {
		t.Fatal("Save returned empty recall token")
	}

	got, err := c.Cases.Recall(context.Background(), put.ID, put.RecallToken)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if string(got.Body) != "encrypted-payload" {
		t.Errorf("body = %q, want %q", got.Body, "encrypted-payload")
	}
	if got.Product != "zyins" {
		t.Errorf("Product = %q, want zyins", got.Product)
	}
}

func TestCasesService_Recall_WrongToken_ReturnsNotFound(t *testing.T) {
	mock := newMockCaseStorage()
	c := newCaseStorageTestClient(t, WithCaseStorage(mock))

	put, err := c.Cases.Save(context.Background(), cases.CaseRecord{Product: "zyins", Body: []byte("x")})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, err = c.Cases.Recall(context.Background(), put.ID, "wrong-token")
	if !errors.Is(err, cases.ErrNotFound) {
		t.Errorf("Recall with wrong token: want ErrNotFound, got %v", err)
	}
}

func TestCasesService_Open_StillDelegatesToRecall(t *testing.T) {
	// Open is the deprecated alias; verify it still works for backward
	// compatibility until the v1.0 removal window.
	mock := newMockCaseStorage()
	c := newCaseStorageTestClient(t, WithCaseStorage(mock))

	put, err := c.Cases.Save(context.Background(), cases.CaseRecord{Product: "zyins", Body: []byte("y")})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := c.Cases.Open(context.Background(), put.ID, put.RecallToken)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got.Body) != "y" {
		t.Errorf("body = %q, want y", got.Body)
	}
}
