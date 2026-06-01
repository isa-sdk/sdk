// Package zyins — lenient per-element parsing for the v3 datasets
// product-slice fields (products_by_family, discontinued_products,
// state_derivatives).
//
// These slices are decoded element-by-element so a single malformed
// entry (a non-integer epoch, a non-string state, a row missing its id)
// skips only that entry rather than aborting the whole bundle decode.
// This mirrors the TS/Python/PHP/C# parsers, which all skip-and-continue;
// a strict typed json.Unmarshal would instead return an
// UnmarshalTypeError and leave the consumer with no bundle at all.

package zyins

import (
	"bytes"
	"encoding/json"
	"math"
)

// float64Int64Ceiling is the smallest float64 strictly greater than
// math.MaxInt64. math.MaxInt64 (2^63-1) is not representable as a float64 and
// rounds up to exactly 2^63, so a float epoch at-or-above this value would
// overflow the int64(f) cast. Used as a strict upper bound on integer-valued
// float epochs.
const float64Int64Ceiling = float64(math.MaxInt64)

// rawProductRow is the minimal product-row shape parsed from a
// products_by_family entry. Only id and name are surfaced today.
type rawProductRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// decodeRawObject parses a JSON object into a slug → raw-value map,
// preserving each value as raw JSON for per-element parsing. A null,
// empty, or non-object payload yields nil.
func decodeRawObject(raw json.RawMessage) map[string]json.RawMessage {
	if isBlankRaw(raw) {
		return nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// decodeRawArray parses a JSON array into its raw element slice for
// per-element parsing. A null, empty, or non-array payload yields nil.
func decodeRawArray(raw json.RawMessage) []json.RawMessage {
	if isBlankRaw(raw) {
		return nil
	}
	var out []json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// decodeRawProductRow parses a single products_by_family element. A
// non-object element yields the zero row (skipped by the caller's
// empty-id/name check).
func decodeRawProductRow(raw json.RawMessage) rawProductRow {
	var row rawProductRow
	_ = json.Unmarshal(raw, &row)
	return row
}

// integerEpochFromRaw parses a discontinued-product value as an
// integer-valued unix-epoch second. It accepts integer-valued JSON
// numbers in any notation (1700000000, 1.7e9, 1700000000.0) and rejects
// genuine fractional values (1700000000.5), booleans, strings, and null.
// The bool result reports whether the value was a valid integer epoch.
//
// Matching C#'s TryGetInt64 (which also accepts 1.7e9 and 1700000000.0
// but rejects fractionals), the rule is "integer-valued number", not
// "JSON written without a decimal point" — so the wire is free to emit
// either notation without silent data loss.
//
// Out-of-range guard: the epoch is int64. An integer-valued float that
// overflows int64 is rejected rather than wrapped — int64(f) on such a
// value is implementation-defined and would surface a silently-wrong
// epoch. C#'s TryIntegerEpoch applies the same long.MinValue/long.MaxValue
// bound, so both parsers drop an out-of-range epoch identically. (The
// num.Int64() branch already rejects overflow by returning an error.)
func integerEpochFromRaw(raw json.RawMessage) (int64, bool) {
	if isBlankRaw(raw) {
		return 0, false
	}
	var num json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&num); err != nil {
		return 0, false
	}
	if i, err := num.Int64(); err == nil {
		return i, true
	}
	f, err := num.Float64()
	if err != nil || math.Trunc(f) != f || math.IsInf(f, 0) {
		return 0, false
	}
	// int64(f) is implementation-defined once f leaves the int64 range. The
	// upper bound is a strict `>=`: math.MaxInt64 rounds up to 2^63 as a
	// float64, so any f at-or-above that threshold would overflow on the cast.
	if f < math.MinInt64 || f >= float64Int64Ceiling {
		return 0, false
	}
	return int64(f), true
}

// isBlankRaw reports whether a raw JSON message is empty or the literal
// null — both of which mean "the field was absent or explicitly null".
func isBlankRaw(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// isJSONArray reports whether raw is a JSON array (its first non-space byte
// is '['). Used to skip a products_by_family family whose value is not an
// array: an empty array [] is a valid array and is kept (as an empty list),
// but a non-array value (number, string, object) is dropped so the family key
// never appears — matching the TS/Python/PHP parsers.
func isJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}
