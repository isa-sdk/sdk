package reference

// Double Metaphone phonetic encoder — Lawrence Philips' algorithm, ported
// branch-for-branch from the SDK's TypeScript reference
// (packages/ts/src/zyins/reference/_doubleMetaphone.ts), which itself
// mirrors the Apache Commons Codec implementation.
//
// doubleMetaphone returns a primary + alternate phonetic code pair. Two
// strings that sound alike share a code (sertaline and sertraline both
// encode SRTRLN; tylonol and tylenol both TLNL), which lets the fuzzy
// matcher recover misspellings that edit distance alone misses.
//
// This file is a cross-language contract surface: it MUST reproduce the
// codes pinned in doubleMetaphone.vectors.ts for every fixture term. The
// vector parity test (doublemetaphone_test.go) enforces that contract.
//
// Determinism notes:
//   - Input is upper-cased with strings.ToUpper (locale-invariant for
//     ASCII) before encoding.
//   - Only ASCII A–Z is processed; any other character is dropped, so the
//     caller is responsible for NFC-normalizing before calling.

import "strings"

// defaultMaxCodeLen is the phonetic code length. Classic Double Metaphone
// truncates to 4, which is too lossy for the long compound terms in the
// medical catalog. A 6-symbol code keeps near-homophone drug names
// colliding while staying short enough to pool genuine homophones. This is
// the value the cross-language vector fixture pins.
const defaultMaxCodeLen = 6

const (
	dmVowels                = "AEIOUY"
	dmLRNMBHFVWSpace        = " BHFVW"
	dmLTKSNMBZ              = "LTKSNMBZ"
	dmTieBreakIDPlaceholder = "￿"
)

// dmSilentStart holds the silent leading clusters Philips strips.
var dmSilentStart = [...]string{"GN", "KN", "PN", "WR", "PS"}

// dmESEPEtc holds the two-letter clusters that trigger the G-at-start
// soft-G branch.
var dmESEPEtc = [...]string{
	"ES", "EP", "EB", "EL", "EY", "IB", "IL", "IN", "IE", "EI", "ER",
}

// doubleMetaphoneCode is the primary + alternate phonetic code pair.
// Primary equals Alternate when the word has no alternate reading.
type doubleMetaphoneCode struct {
	primary   string
	alternate string
}

// codeBuilder accumulates the primary + alternate codes, encapsulating the
// "append the same to both / append divergent to each" pattern.
type codeBuilder struct {
	primary   strings.Builder
	alternate strings.Builder
	maxLength int
}

// append adds primary to the primary code and alternate to the alternate
// code. When alt is empty, the same symbol is appended to both.
func (b *codeBuilder) append(primary, alternate string) {
	b.primary.WriteString(primary)
	b.alternate.WriteString(alternate)
}

// appendBoth adds the same symbol to both codes.
func (b *codeBuilder) appendBoth(symbol string) {
	b.primary.WriteString(symbol)
	b.alternate.WriteString(symbol)
}

func (b *codeBuilder) done() bool {
	return b.primary.Len() >= b.maxLength && b.alternate.Len() >= b.maxLength
}

func (b *codeBuilder) build() doubleMetaphoneCode {
	return doubleMetaphoneCode{
		primary:   truncate(b.primary.String(), b.maxLength),
		alternate: truncate(b.alternate.String(), b.maxLength),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// word is a cursor over the upper-cased input with bounds-safe accessors
// that mirror the TS Word helper. value is ASCII-only, so byte indexing is
// equivalent to the TS UTF-16 charAt over A–Z.
type word struct {
	value string
}

func (w word) length() int { return len(w.value) }

func (w word) at(index int) byte {
	if index < 0 || index >= len(w.value) {
		return 0
	}
	return w.value[index]
}

func (w word) slice(start, end int) string {
	lo := start
	if lo < 0 {
		lo = 0
	}
	hi := end
	if hi > len(w.value) {
		hi = len(w.value)
	}
	if hi <= lo {
		return ""
	}
	return w.value[lo:hi]
}

func (w word) isVowel(index int) bool {
	ch := w.at(index)
	return ch != 0 && strings.IndexByte(dmVowels, ch) >= 0
}

func (w word) isSlavoGermanic() bool {
	return strings.ContainsAny(w.value, "WK") ||
		strings.Contains(w.value, "CZ") ||
		strings.Contains(w.value, "WITZ")
}

func (w word) contains(start, length int, candidates ...string) bool {
	target := w.slice(start, start+length)
	for _, c := range candidates {
		if c == target {
			return true
		}
	}
	return false
}

// doubleMetaphone encodes input into its Double Metaphone primary +
// alternate codes. Non-letters are dropped before encoding; an empty or
// letter-free input yields empty codes.
func doubleMetaphone(input string) doubleMetaphoneCode {
	cleaned := cleanForMetaphone(input)
	if cleaned == "" {
		return doubleMetaphoneCode{}
	}

	w := word{value: cleaned}
	code := &codeBuilder{maxLength: defaultMaxCodeLen}
	index := skipSilentStart(w)
	if w.at(0) == 'X' {
		code.appendBoth("S")
		index = 1
	}

	for index < w.length() && !code.done() {
		index = stepMetaphone(w, code, index)
	}
	return code.build()
}

// cleanForMetaphone upper-cases the input and drops every byte that is not
// ASCII A–Z. The TS port also recognizes Ç and Ñ; those are multi-byte in
// UTF-8 and are handled before the strip so their branches stay reachable.
func cleanForMetaphone(input string) string {
	upper := strings.ToUpper(input)
	var b strings.Builder
	b.Grow(len(upper))
	for _, r := range upper {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r == 'Ç':
			b.WriteByte(metaphoneCedilla)
		case r == 'Ñ':
			b.WriteByte(metaphoneEnye)
		}
	}
	return b.String()
}

// metaphoneCedilla / metaphoneEnye are private sentinel bytes standing in
// for Ç / Ñ inside the ASCII-only cursor so their step branches remain
// reachable without widening the cursor to runes. They never collide with
// A–Z and never appear in output codes.
const (
	metaphoneCedilla byte = 0x01
	metaphoneEnye    byte = 0x02
)

// skipSilentStart skips the silent leading clusters (GN, KN, PN, WR, PS).
func skipSilentStart(w word) int {
	head := w.slice(0, 2)
	for _, cluster := range dmSilentStart {
		if cluster == head {
			return 1
		}
	}
	return 0
}

// stepMetaphone encodes the character at index, appending zero or more code
// symbols, and returns the next index to process.
func stepMetaphone(w word, code *codeBuilder, index int) int {
	ch := w.at(index)
	switch ch {
	case 'A', 'E', 'I', 'O', 'U', 'Y':
		if index == 0 {
			code.appendBoth("A")
		}
		return index + 1
	case 'B':
		code.appendBoth("P")
		if w.at(index+1) == 'B' {
			return index + 2
		}
		return index + 1
	case 'C':
		return stepC(w, code, index)
	case metaphoneCedilla:
		code.appendBoth("S")
		return index + 1
	case 'D':
		return stepD(w, code, index)
	case 'F':
		code.appendBoth("F")
		if w.at(index+1) == 'F' {
			return index + 2
		}
		return index + 1
	case 'G':
		return stepG(w, code, index)
	case 'H':
		return stepH(w, code, index)
	case 'J':
		return stepJ(w, code, index)
	case 'K':
		code.appendBoth("K")
		if w.at(index+1) == 'K' {
			return index + 2
		}
		return index + 1
	case 'L':
		return stepL(w, code, index)
	case 'M':
		code.appendBoth("M")
		if isMSilentDoubled(w, index) {
			return index + 2
		}
		return index + 1
	case 'N':
		code.appendBoth("N")
		if w.at(index+1) == 'N' {
			return index + 2
		}
		return index + 1
	case metaphoneEnye:
		code.appendBoth("N")
		return index + 1
	case 'P':
		return stepP(w, code, index)
	case 'Q':
		code.appendBoth("K")
		if w.at(index+1) == 'Q' {
			return index + 2
		}
		return index + 1
	case 'R':
		return stepR(w, code, index)
	case 'S':
		return stepS(w, code, index)
	case 'T':
		return stepT(w, code, index)
	case 'V':
		code.appendBoth("F")
		if w.at(index+1) == 'V' {
			return index + 2
		}
		return index + 1
	case 'W':
		return stepW(w, code, index)
	case 'X':
		return stepX(w, code, index)
	case 'Z':
		return stepZ(w, code, index)
	default:
		return index + 1
	}
}
