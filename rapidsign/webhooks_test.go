package rapidsign

import (
	"errors"
	"net/http"
	"testing"
)

// TestWebhooksVerify_NotImplementedShape asserts the exact error type
// and HTTP status surfaced by the reserved Verify method. Consumers can
// rely on errors.As(*NotImplementedError) until the server surface lands.
func TestWebhooksVerify_NotImplementedShape(t *testing.T) {
	t.Parallel()
	c, err := New("isa_test_token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Webhooks.Verify(nil, http.Header{}, "secret")
	var ni *NotImplementedError
	if !errors.As(err, &ni) {
		t.Fatalf("err = %T, want *NotImplementedError", err)
	}
	if ni.Err.HTTPStatus != http.StatusNotImplemented {
		t.Errorf("HTTPStatus = %d, want 501", ni.Err.HTTPStatus)
	}
	if ni.Err.Code != ErrorCodeNotImplemented {
		t.Errorf("Code = %q, want not_implemented", ni.Err.Code)
	}
}
