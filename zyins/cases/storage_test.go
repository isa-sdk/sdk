package cases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeDoer captures the last request and returns canned responses.
type fakeDoer struct {
	postPath string
	postBody putWireBody
	postResp []byte
	postErr  error

	getPath string
	getResp []byte
	getErr  error
}

func (f *fakeDoer) Post(_ context.Context, path string, body any) ([]byte, error) {
	f.postPath = path
	raw, _ := json.Marshal(body)
	_ = json.Unmarshal(raw, &f.postBody)
	if f.postErr != nil {
		return nil, f.postErr
	}
	return f.postResp, nil
}

func (f *fakeDoer) Get(_ context.Context, path string) ([]byte, error) {
	f.getPath = path
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResp, nil
}

func TestZeroKnowledgeCaseStorage_PutRequiresProduct(t *testing.T) {
	store := NewZeroKnowledgeCaseStorage(&fakeDoer{})
	_, err := store.Put(context.Background(), CaseRecord{Body: []byte("payload")})
	if err == nil {
		t.Fatal("expected error for missing product")
	}
	if !strings.Contains(err.Error(), "Product is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestZeroKnowledgeCaseStorage_RoundTrip(t *testing.T) {
	const product = "zyins"
	plaintext := []byte(`{"applicant":"Jane Smith"}`)

	doer := &fakeDoer{
		postResp: []byte(`{"data":{"id":"case_abc"}}`),
	}
	store := NewZeroKnowledgeCaseStorage(doer)

	putResult, err := store.Put(context.Background(), CaseRecord{
		Product: product,
		Body:    plaintext,
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if putResult.ID != "case_abc" {
		t.Errorf("ID = %q, want case_abc", putResult.ID)
	}
	if putResult.RecallToken == "" {
		t.Fatal("RecallToken empty")
	}
	if doer.postPath != "/v1/cases" {
		t.Errorf("post path = %q, want /v1/cases", doer.postPath)
	}
	// The wire body must NOT contain the cleartext.
	if bytes.Contains([]byte(doer.postBody.Ciphertext), plaintext) {
		t.Error("ciphertext base64 leaks cleartext substring")
	}
	if doer.postBody.Product != product {
		t.Errorf("wire product = %q, want %q", doer.postBody.Product, product)
	}

	// Set up the GET response from the captured POST body so the round
	// trip uses the SAME nonce/tag/ciphertext.
	getEnvelope := map[string]any{
		"data": map[string]any{
			"product":    doer.postBody.Product,
			"ciphertext": doer.postBody.Ciphertext,
			"iv":         doer.postBody.IV,
			"tag":        doer.postBody.Tag,
		},
	}
	getRaw, _ := json.Marshal(getEnvelope)
	doer.getResp = getRaw

	record, err := store.Get(context.Background(), putResult.ID, putResult.RecallToken)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if doer.getPath != "/v1/cases/"+putResult.ID {
		t.Errorf("get path = %q", doer.getPath)
	}
	if !bytes.Equal(record.Body, plaintext) {
		t.Errorf("decrypted body = %q, want %q", record.Body, plaintext)
	}
	if record.Product != product {
		t.Errorf("product = %q, want %q", record.Product, product)
	}
}

func TestZeroKnowledgeCaseStorage_GetWrongTokenReturnsNotFound(t *testing.T) {
	doer := &fakeDoer{
		postResp: []byte(`{"data":{"id":"case_xyz"}}`),
	}
	store := NewZeroKnowledgeCaseStorage(doer)

	put, err := store.Put(context.Background(), CaseRecord{Product: "zyins", Body: []byte("secret")})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	getEnvelope := map[string]any{
		"data": map[string]any{
			"product":    doer.postBody.Product,
			"ciphertext": doer.postBody.Ciphertext,
			"iv":         doer.postBody.IV,
			"tag":        doer.postBody.Tag,
		},
	}
	doer.getResp, _ = json.Marshal(getEnvelope)

	// Tamper with the recall token (flip first char).
	tampered := flipChar(put.RecallToken[0]) + put.RecallToken[1:]
	_, err = store.Get(context.Background(), put.ID, tampered)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong token: want ErrNotFound, got %v", err)
	}
}

// statusErr mirrors the structured transport error: it carries the HTTP
// status the cases package matches on. The real zyins.*Error satisfies
// the same interface{ StatusCode() int } shape.
type statusErr struct {
	status int
	msg    string
}

func (e *statusErr) Error() string   { return e.msg }
func (e *statusErr) StatusCode() int { return e.status }

func TestZeroKnowledgeCaseStorage_Get404ReturnsNotFound(t *testing.T) {
	doer := &fakeDoer{
		getErr: &statusErr{status: 404, msg: "zyins: GET /v1/case/missing [op=cases_get]: not found"},
	}
	store := NewZeroKnowledgeCaseStorage(doer)
	// Need a recall token shaped correctly enough to base64-decode.
	_, err := store.Get(context.Background(), "missing", "AAAA")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("404 transport error: want ErrNotFound, got %v", err)
	}
}

// TestZeroKnowledgeCaseStorage_Non404WithFourZeroFourInMessageIsNotNotFound
// guards the substring-matching regression: a 500 (or any non-404)
// whose message or id happens to contain "404" must surface as a real
// error, never be silently swallowed as an absent record.
func TestZeroKnowledgeCaseStorage_Non404WithFourZeroFourInMessageIsNotNotFound(t *testing.T) {
	doer := &fakeDoer{
		getErr: &statusErr{status: 500, msg: "zyins: GET /v1/case/case-404-xyz [op=cases_get]: internal error 404 mentioned"},
	}
	store := NewZeroKnowledgeCaseStorage(doer)
	_, err := store.Get(context.Background(), "case-404-xyz", "AAAA")
	if errors.Is(err, ErrNotFound) {
		t.Errorf("500 error containing '404' must not map to ErrNotFound; got ErrNotFound")
	}
	if err == nil {
		t.Error("expected the 500 error to surface, got nil")
	}
}

func TestZeroKnowledgeCaseStorage_ProductBindingPreventsCrossProduct(t *testing.T) {
	doer := &fakeDoer{
		postResp: []byte(`{"data":{"id":"case_pp"}}`),
	}
	store := NewZeroKnowledgeCaseStorage(doer)
	put, err := store.Put(context.Background(), CaseRecord{Product: "zyins", Body: []byte("data")})
	if err != nil {
		t.Fatal(err)
	}
	// Server claims a DIFFERENT product on read.
	getEnvelope := map[string]any{
		"data": map[string]any{
			"product":    "eapp",
			"ciphertext": doer.postBody.Ciphertext,
			"iv":         doer.postBody.IV,
			"tag":        doer.postBody.Tag,
		},
	}
	doer.getResp, _ = json.Marshal(getEnvelope)
	_, err = store.Get(context.Background(), put.ID, put.RecallToken)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-product: want ErrNotFound, got %v", err)
	}
}

// flipChar returns a different ASCII character in the base64url alphabet.
func flipChar(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}
