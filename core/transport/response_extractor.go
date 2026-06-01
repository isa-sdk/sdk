package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Envelope mirrors the response envelope defined in ADR-012:
//
//	{ "object": "...", "livemode": bool, "request_id": "...", "data": ... }
//
// The generated per-product client types deserialize the inner `data`
// payload directly; ExtractData peels the envelope so that workflow.
type Envelope struct {
	Object    string          `json:"object"`
	Livemode  bool            `json:"livemode"`
	RequestID string          `json:"request_id"`
	Data      json.RawMessage `json:"data"`
}

// ErrEnvelopeMissingData indicates the response parsed as JSON but did
// not carry a `data` field. Surfaced to consumers as a typed error so
// retries can be skipped (the response is structurally malformed, not
// transiently broken).
var ErrEnvelopeMissingData = errors.New("transport: response envelope has no data field")

// ExtractData decodes the envelope from r and writes the inner data
// payload into out. Closing r is the caller's responsibility — the
// helper mirrors json.Decoder, which does not own its reader.
func ExtractData(r io.Reader, out any) error {
	if r == nil {
		return errors.New("transport: ExtractData requires a non-nil reader")
	}
	if out == nil {
		return errors.New("transport: ExtractData requires a non-nil destination")
	}
	var env Envelope
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		return fmt.Errorf("transport: failed to decode response envelope: %w", err)
	}
	if len(env.Data) == 0 || bytes.Equal(env.Data, []byte("null")) {
		return ErrEnvelopeMissingData
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("transport: failed to decode envelope data field: %w", err)
	}
	return nil
}

// ExtractEnvelope decodes the envelope without touching the inner data
// payload. Useful when the caller wants request_id for logging before
// committing to a concrete type for data.
func ExtractEnvelope(r io.Reader) (*Envelope, error) {
	if r == nil {
		return nil, errors.New("transport: ExtractEnvelope requires a non-nil reader")
	}
	var env Envelope
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		return nil, fmt.Errorf("transport: failed to decode response envelope: %w", err)
	}
	return &env, nil
}
