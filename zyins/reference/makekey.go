package reference

import "strings"

// makeKey mirrors the server-side MakeKey normalizer in
// go/zyins/models/makekey.go: uppercase, then strip every byte that is
// not ASCII alphanumeric. "High Blood Pressure" → "HIGHBLOODPRESSURE".
//
// Intentionally unexported. The reference package is the only path that
// calls it; consumers must use Match.
func makeKey(text string) string {
	upper := strings.ToUpper(text)
	var b strings.Builder
	b.Grow(len(upper))
	for i := 0; i < len(upper); i++ {
		ch := upper[i]
		isDigit := ch >= '0' && ch <= '9'
		isUpper := ch >= 'A' && ch <= 'Z'
		if isDigit || isUpper {
			b.WriteByte(ch)
		}
	}
	return b.String()
}
