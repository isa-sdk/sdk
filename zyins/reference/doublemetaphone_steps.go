package reference

// Per-consonant step functions for the Double Metaphone encoder. Split
// from doublemetaphone.go to keep each file focused; the branch structure
// mirrors packages/ts/src/zyins/reference/_doubleMetaphone.ts exactly.

import "strings"

func stepC(w word, code *codeBuilder, index int) int {
	if conditionC0(w, index) {
		code.appendBoth("K")
		return index + 2
	}
	if index == 0 && w.contains(index, 6, "CAESAR") {
		code.appendBoth("S")
		return index + 2
	}
	if w.contains(index, 2, "CH") {
		return stepCH(w, code, index)
	}
	if w.contains(index, 2, "CZ") && !w.contains(index-2, 4, "WICZ") {
		code.append("S", "X")
		return index + 2
	}
	if w.contains(index+1, 3, "CIA") {
		code.appendBoth("X")
		return index + 3
	}
	if w.contains(index, 2, "CC") && !(index == 1 && w.at(0) == 'M') {
		return stepCC(w, code, index)
	}
	if w.contains(index, 2, "CK", "CG", "CQ") {
		code.appendBoth("K")
		return index + 2
	}
	if w.contains(index, 2, "CI", "CE", "CY") {
		if w.contains(index, 3, "CIO", "CIE", "CIA") {
			code.append("S", "X")
		} else {
			code.appendBoth("S")
		}
		return index + 2
	}
	code.appendBoth("K")
	if w.contains(index+1, 2, " C", " Q", " G") {
		return index + 3
	}
	if w.contains(index+1, 1, "C", "K", "Q") && !w.contains(index+1, 2, "CE", "CI") {
		return index + 2
	}
	return index + 1
}

func conditionC0(w word, index int) bool {
	if w.contains(index, 4, "CHIA") {
		return true
	}
	if index <= 1 {
		return false
	}
	if w.isVowel(index - 2) {
		return false
	}
	if !w.contains(index-1, 3, "ACH") {
		return false
	}
	c := w.at(index + 2)
	return (c != 'I' && c != 'E') || w.contains(index-2, 6, "BACHER", "MACHER")
}

func stepCC(w word, code *codeBuilder, index int) int {
	if w.contains(index+2, 1, "I", "E", "H") && !w.contains(index+2, 2, "HU") {
		if (index == 1 && w.at(index-1) == 'A') ||
			w.contains(index-1, 5, "UCCEE", "UCCES") {
			code.appendBoth("KS")
		} else {
			code.appendBoth("X")
		}
		return index + 3
	}
	code.appendBoth("K")
	return index + 2
}

func stepCH(w word, code *codeBuilder, index int) int {
	if index > 0 && w.contains(index, 4, "CHAE") {
		code.append("K", "X")
		return index + 2
	}
	if conditionCH0(w, index) || conditionCH1(w, index) {
		code.appendBoth("K")
		return index + 2
	}
	if index > 0 {
		if w.contains(0, 2, "MC") {
			code.append("K", "K")
		} else {
			code.append("X", "K")
		}
	} else {
		code.appendBoth("X")
	}
	return index + 2
}

func conditionCH0(w word, index int) bool {
	if index != 0 {
		return false
	}
	if !w.contains(index+1, 5, "HARAC", "HARIS") &&
		!w.contains(index+1, 3, "HOR", "HYM", "HIA", "HEM") {
		return false
	}
	return !w.contains(0, 5, "CHORE")
}

func conditionCH1(w word, index int) bool {
	lrnmbhfvwSpace := strings.Split(dmLRNMBHFVWSpace, "")
	return w.contains(0, 4, "VAN ", "VON ") ||
		w.contains(0, 3, "SCH") ||
		w.contains(index-2, 6, "ORCHES", "ARCHIT", "ORCHID") ||
		w.contains(index+2, 1, "T", "S") ||
		((w.contains(index-1, 1, "A", "O", "U", "E") || index == 0) &&
			(w.contains(index+2, 1, lrnmbhfvwSpace...) ||
				index+1 == w.length()-1))
}

func stepD(w word, code *codeBuilder, index int) int {
	if w.contains(index, 2, "DG") {
		if w.contains(index+2, 1, "I", "E", "Y") {
			code.appendBoth("J")
			return index + 3
		}
		code.appendBoth("TK")
		return index + 2
	}
	code.appendBoth("T")
	if w.contains(index, 2, "DT", "DD") {
		return index + 2
	}
	return index + 1
}

func stepG(w word, code *codeBuilder, index int) int {
	if w.at(index+1) == 'H' {
		return stepGH(w, code, index)
	}
	if w.at(index+1) == 'N' {
		return stepGN(w, code, index)
	}
	if w.contains(index+1, 2, "LI") && !w.isSlavoGermanic() {
		code.append("KL", "L")
		return index + 2
	}
	if index == 0 &&
		(w.at(index+1) == 'Y' || w.contains(index+1, 2, dmESEPEtc[:]...)) {
		code.append("K", "J")
		return index + 2
	}
	if (w.contains(index+1, 2, "ER") || w.at(index+1) == 'Y') &&
		!w.contains(0, 6, "DANGER", "RANGER", "MANGER") &&
		!w.contains(index-1, 1, "E", "I") &&
		!w.contains(index-1, 3, "RGY", "OGY") {
		code.append("K", "J")
		return index + 2
	}
	if w.contains(index+1, 1, "E", "I", "Y") ||
		w.contains(index-1, 4, "AGGI", "OGGI") {
		switch {
		case w.contains(0, 4, "VAN ", "VON ") ||
			w.contains(0, 3, "SCH") ||
			w.contains(index+1, 2, "ET"):
			code.appendBoth("K")
		case w.contains(index+1, 3, "IER"):
			code.appendBoth("J")
		default:
			code.append("J", "K")
		}
		return index + 2
	}
	code.appendBoth("K")
	if w.at(index+1) == 'G' {
		return index + 2
	}
	return index + 1
}

func stepGH(w word, code *codeBuilder, index int) int {
	if index > 0 && !w.isVowel(index-1) {
		code.appendBoth("K")
		return index + 2
	}
	if index == 0 {
		if w.at(index+2) == 'I' {
			code.appendBoth("J")
		} else {
			code.appendBoth("K")
		}
		return index + 2
	}
	if (index > 1 && w.contains(index-2, 1, "B", "H", "D")) ||
		(index > 2 && w.contains(index-3, 1, "B", "H", "D")) ||
		(index > 3 && w.contains(index-4, 1, "B", "H")) {
		return index + 2
	}
	if index > 2 && w.at(index-1) == 'U' &&
		w.contains(index-3, 1, "C", "G", "L", "R", "T") {
		code.appendBoth("F")
	} else if index > 0 && w.at(index-1) != 'I' {
		code.appendBoth("K")
	}
	return index + 2
}

func stepGN(w word, code *codeBuilder, index int) int {
	switch {
	case index == 1 && w.isVowel(0) && !w.isSlavoGermanic():
		code.append("KN", "N")
	case !w.contains(index+2, 2, "EY") && w.at(index+1) != 'Y' && !w.isSlavoGermanic():
		code.append("N", "KN")
	default:
		code.appendBoth("KN")
	}
	return index + 2
}

func stepH(w word, code *codeBuilder, index int) int {
	if (index == 0 || w.isVowel(index-1)) && w.isVowel(index+1) {
		code.appendBoth("H")
		return index + 2
	}
	return index + 1
}

func stepJ(w word, code *codeBuilder, index int) int {
	if w.contains(index, 4, "JOSE") || w.contains(0, 4, "SAN ") {
		if (index == 0 && w.at(index+4) == ' ') || w.contains(0, 4, "SAN ") {
			code.appendBoth("H")
		} else {
			code.append("J", "H")
		}
		return index + 1
	}
	switch {
	case index == 0 && !w.contains(index, 4, "JOSE"):
		code.append("J", "A")
	case w.isVowel(index-1) && !w.isSlavoGermanic() &&
		(w.at(index+1) == 'A' || w.at(index+1) == 'O'):
		code.append("J", "H")
	case index == w.length()-1:
		code.append("J", "")
	case !w.contains(index+1, 1, strings.Split(dmLTKSNMBZ, "")...) &&
		!w.contains(index-1, 1, "S", "K", "L"):
		code.appendBoth("J")
	}
	if w.at(index+1) == 'J' {
		return index + 2
	}
	return index + 1
}

func stepL(w word, code *codeBuilder, index int) int {
	if w.at(index+1) == 'L' {
		if conditionL0(w, index) {
			code.append("L", "")
		} else {
			code.appendBoth("L")
		}
		return index + 2
	}
	code.appendBoth("L")
	return index + 1
}

func conditionL0(w word, index int) bool {
	if index == w.length()-3 &&
		w.contains(index-1, 4, "ILLO", "ILLA", "ALLE") {
		return true
	}
	return (w.contains(w.length()-2, 2, "AS", "OS") ||
		w.contains(w.length()-1, 1, "A", "O")) &&
		w.contains(index-1, 4, "ALLE")
}

func stepP(w word, code *codeBuilder, index int) int {
	if w.at(index+1) == 'H' {
		code.appendBoth("F")
		return index + 2
	}
	code.appendBoth("P")
	if w.contains(index+1, 1, "P", "B") {
		return index + 2
	}
	return index + 1
}

func stepR(w word, code *codeBuilder, index int) int {
	if index == w.length()-1 &&
		!w.isSlavoGermanic() &&
		w.contains(index-2, 2, "IE") &&
		!w.contains(index-4, 2, "ME", "MA") {
		code.append("", "R")
	} else {
		code.appendBoth("R")
	}
	if w.at(index+1) == 'R' {
		return index + 2
	}
	return index + 1
}

func stepS(w word, code *codeBuilder, index int) int {
	if w.contains(index-1, 3, "ISL", "YSL") {
		return index + 1
	}
	if index == 0 && w.contains(index, 5, "SUGAR") {
		code.append("X", "S")
		return index + 1
	}
	if w.contains(index, 2, "SH") {
		if w.contains(index+1, 4, "HEIM", "HOEK", "HOLM", "HOLZ") {
			code.appendBoth("S")
		} else {
			code.appendBoth("X")
		}
		return index + 2
	}
	if w.contains(index, 3, "SIO", "SIA") || w.contains(index, 4, "SIAN") {
		if w.isSlavoGermanic() {
			code.appendBoth("S")
		} else {
			code.append("S", "X")
		}
		return index + 3
	}
	if (index == 0 && w.contains(index+1, 1, "M", "N", "L", "W")) ||
		w.contains(index+1, 1, "Z") {
		code.append("S", "X")
		if w.at(index+1) == 'Z' {
			return index + 2
		}
		return index + 1
	}
	if w.contains(index, 2, "SC") {
		return stepSC(w, code, index)
	}
	if index == w.length()-1 && w.contains(index-2, 2, "AI", "OI") {
		code.append("", "S")
	} else {
		code.appendBoth("S")
	}
	if w.contains(index+1, 1, "S", "Z") {
		return index + 2
	}
	return index + 1
}

func stepSC(w word, code *codeBuilder, index int) int {
	if w.at(index+2) == 'H' {
		switch {
		case w.contains(index+3, 2, "OO", "ER", "EN", "UY", "ED", "EM"):
			if w.contains(index+3, 2, "ER", "EN") {
				code.append("X", "SK")
			} else {
				code.append("SK", "SK")
			}
		case index == 0 && !w.isVowel(3) && w.at(3) != 'W':
			code.append("X", "S")
		default:
			code.appendBoth("X")
		}
		return index + 3
	}
	if w.contains(index+2, 1, "I", "E", "Y") {
		code.appendBoth("S")
		return index + 3
	}
	code.appendBoth("SK")
	return index + 3
}

func stepT(w word, code *codeBuilder, index int) int {
	if w.contains(index, 4, "TION") {
		code.appendBoth("X")
		return index + 3
	}
	if w.contains(index, 3, "TIA", "TCH") {
		code.appendBoth("X")
		return index + 3
	}
	if w.contains(index, 2, "TH") || w.contains(index, 3, "TTH") {
		if w.contains(index+2, 2, "OM", "AM") ||
			w.contains(0, 4, "VAN ", "VON ") ||
			w.contains(0, 3, "SCH") {
			code.appendBoth("T")
		} else {
			code.append("0", "T")
		}
		return index + 2
	}
	code.appendBoth("T")
	if w.contains(index+1, 1, "T", "D") {
		return index + 2
	}
	return index + 1
}

func stepW(w word, code *codeBuilder, index int) int {
	if w.contains(index, 2, "WR") {
		code.appendBoth("R")
		return index + 2
	}
	if index == 0 && (w.isVowel(index+1) || w.contains(index, 2, "WH")) {
		if w.isVowel(index + 1) {
			code.append("A", "F")
		} else {
			code.appendBoth("A")
		}
		return index + 1
	}
	if (index == w.length()-1 && w.isVowel(index-1)) ||
		w.contains(index-1, 5, "EWSKI", "EWSKY", "OWSKI", "OWSKY") ||
		w.contains(0, 3, "SCH") {
		code.append("", "F")
		return index + 1
	}
	if w.contains(index, 4, "WICZ", "WITZ") {
		code.append("TS", "FX")
		return index + 4
	}
	return index + 1
}

func stepX(w word, code *codeBuilder, index int) int {
	if !(index == w.length()-1 &&
		(w.contains(index-3, 3, "IAU", "EAU") || w.contains(index-2, 2, "AU", "OU"))) {
		code.appendBoth("KS")
	}
	if w.contains(index+1, 1, "C", "X") {
		return index + 2
	}
	return index + 1
}

func stepZ(w word, code *codeBuilder, index int) int {
	if w.at(index+1) == 'H' {
		code.appendBoth("J")
		return index + 2
	}
	if w.contains(index+1, 2, "ZO", "ZI", "ZA") ||
		(w.isSlavoGermanic() && index > 0 && w.at(index-1) != 'T') {
		code.append("S", "TS")
	} else {
		code.appendBoth("S")
	}
	if w.at(index+1) == 'Z' {
		return index + 2
	}
	return index + 1
}

func isMSilentDoubled(w word, index int) bool {
	return (w.contains(index-1, 3, "UMB") &&
		(index+1 == w.length()-1 || w.contains(index+2, 2, "ER"))) ||
		w.at(index+1) == 'M'
}
