package transport

import (
	"errors"
	"strings"
	"testing"
)

type customerData struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func TestExtractData_UnwrapsEnvelopeIntoTypedStruct(t *testing.T) {
	body := `{"object":"customer","livemode":true,"request_id":"req_abc","data":{"id":"cus_1","email":"a@b.com"}}`
	var got customerData
	if err := ExtractData(strings.NewReader(body), &got); err != nil {
		t.Fatalf("ExtractData: %v", err)
	}
	if got.ID != "cus_1" || got.Email != "a@b.com" {
		t.Fatalf("data = %+v, want {cus_1 a@b.com}", got)
	}
}

func TestExtractData_NilReader_ReturnsError(t *testing.T) {
	var out customerData
	if err := ExtractData(nil, &out); err == nil {
		t.Fatalf("expected error for nil reader")
	}
}

func TestExtractData_NilDestination_ReturnsError(t *testing.T) {
	if err := ExtractData(strings.NewReader(`{"data":{}}`), nil); err == nil {
		t.Fatalf("expected error for nil destination")
	}
}

func TestExtractData_MissingDataField_ReturnsTypedSentinel(t *testing.T) {
	body := `{"object":"customer","livemode":false,"request_id":"req_xyz"}`
	var out customerData
	err := ExtractData(strings.NewReader(body), &out)
	if !errors.Is(err, ErrEnvelopeMissingData) {
		t.Fatalf("expected ErrEnvelopeMissingData, got %v", err)
	}
}

func TestExtractData_NullDataField_ReturnsTypedSentinel(t *testing.T) {
	body := `{"object":"customer","livemode":false,"request_id":"req_xyz","data":null}`
	var out customerData
	err := ExtractData(strings.NewReader(body), &out)
	if !errors.Is(err, ErrEnvelopeMissingData) {
		t.Fatalf("expected ErrEnvelopeMissingData for null data, got %v", err)
	}
}

func TestExtractData_MalformedJSON_ReturnsWrappedDecodeError(t *testing.T) {
	var out customerData
	err := ExtractData(strings.NewReader("{not json"), &out)
	if err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestExtractEnvelope_ReturnsHeaderFieldsWithoutTouchingData(t *testing.T) {
	body := `{"object":"list","livemode":false,"request_id":"req_42","data":[1,2,3]}`
	env, err := ExtractEnvelope(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ExtractEnvelope: %v", err)
	}
	if env.RequestID != "req_42" || env.Object != "list" {
		t.Fatalf("envelope fields = %+v", env)
	}
	if string(env.Data) != "[1,2,3]" {
		t.Fatalf("data raw = %q", string(env.Data))
	}
}
