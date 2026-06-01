package rapidsign

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	coretransport "github.com/isa-sdk/sdk/core/transport"
)

// decodeEnvelope reads the response body, unwraps the standard
// `{ data: ... }` envelope (per ADR-012), and decodes the inner data
// into out. Empty / null `data` is a structural malformation, surfaced
// as a wrapped error so callers can switch on the underlying
// coretransport.ErrEnvelopeMissingData via errors.Is.
func decodeEnvelope(r io.Reader, out any) error {
	if err := coretransport.ExtractData(r, out); err != nil {
		if errors.Is(err, coretransport.ErrEnvelopeMissingData) {
			return fmt.Errorf("rapidsign: server response had no `data` field: %w", err)
		}
		return fmt.Errorf("rapidsign: failed to decode server response: %w", err)
	}
	return nil
}

// decodeBase64Signature returns the binary bytes of a base64 payload.
// Accepts both standard and URL-safe encodings to tolerate server
// variations.
func decodeBase64Signature(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("rapidsign: signature payload was not valid base64")
}

// errorsAs is a thin alias over errors.As that keeps call sites in
// other files terse without spreading the errors import. The contract
// matches the standard library exactly.
func errorsAs(err error, target any) bool { return errors.As(err, target) }
