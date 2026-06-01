package zyins

import (
	"encoding/json"
	"fmt"
)

// decodeJSONEnvelope parses the full ADR-012 success envelope into a
// generic tree for callers that need the engine's legacy quote-shaped
// payload (meta/results) rather than typed plans[].
func decodeJSONEnvelope(body []byte, op string) (map[string]any, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("zyins: %s response body was empty", op)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode %s envelope: %w", op, err)
	}
	return out, nil
}
