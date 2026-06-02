package reference

import (
	"sort"
	"strings"
)

// checkKey mirrors the engine's CondNameMakeCheckKey in
// go/zyins/utils/strings/normalize.go (and Perl cond_name_make_check_key_xs):
// uppercase, strip every byte that is not ASCII alphanumeric, then SORT
// the surviving bytes ascending. Digits ('0'-'9') sort before letters
// ('A'-'Z'), byte-identical to the engine.
//
// Sorting collapses word order so prefix / suffix / no-space severity
// variants of one concept resolve identically, while severity qualifiers
// stay distinct (MILD vs SEVERE differ in letter multiset):
//
//	"SEVERE COPD"   → "CCDEEOPRSV"
//	"COPD (SEVERE)" → "CCDEEOPRSV"
//	"COPD (MILD)"   → "CCDDILMOP"
//
// Used ONLY for word-order-invariant NAME matching. Opaque catalog ids
// (e.g. HIGHBLOODPRESSURE, cond_<ULID>) are never keyed this way —
// sorting an id would let unrelated ids collide. Id and exact-name
// lookups stay on makeKey.
//
// Intentionally unexported. The reference package is the only path that
// calls it; consumers must use Match.
func checkKey(text string) string {
	upper := strings.ToUpper(text)
	b := make([]byte, 0, len(upper))
	for i := 0; i < len(upper); i++ {
		ch := upper[i]
		isDigit := ch >= '0' && ch <= '9'
		isUpper := ch >= 'A' && ch <= 'Z'
		if isDigit || isUpper {
			b = append(b, ch)
		}
	}
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	return string(b)
}
