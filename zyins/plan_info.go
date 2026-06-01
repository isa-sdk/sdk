package zyins

// Title Case label derivation + typed plan-info item. Mirrors
// packages/ts/src/zyins/planInfoLabel.ts and
// packages/python/src/sah_sdk/zyins/plan_info_label.py.
//
// The post-zyins#349 wire shape carries a server-emitted label per
// item — used verbatim. For pre-#349 bodies (legacy
// map[string][]string shape) the SDK upconverts to the typed array
// surface and synthesizes a label by Title-Casing the snake_case key
// so downstream consumers see exactly one type during the migration
// window.

import (
	"sort"
	"strings"
	"unicode"
)

// PlanInfoItem is one server-canonical entry in a plan's plan_info.
// Iteration is stable — wire array order is preserved exactly.
type PlanInfoItem struct {
	// Key is the stable wire identifier (snake_case).
	Key string
	// Label is the Title Case display string (server-emitted post-#349,
	// synthesized via [TitleCaseLabel] on legacy bodies).
	Label string
	// Values are the URL-decoded value strings in display order.
	Values []string
}

// specialLabels maps lowercased tokens to their canonical display
// form. The TS and Python SDKs carry the identical set; keep them in
// lock-step so a bug in one language translates to a bug in the other.
var specialLabels = map[string]string{
	"eapp": "eApp",
	"url":  "URL",
	"pdf":  "PDF",
	"faq":  "FAQ",
	"api":  "API",
	"id":   "ID",
	"eft":  "EFT",
	"ach":  "ACH",
	"ssn":  "SSN",
}

// TitleCaseLabel converts a snake_case / kebab-case plan-info key to
// its Title Case display form. Special-cases the canonical acronyms
// (eApp, URL, PDF, FAQ, API, ID, EFT, ACH, SSN); all other tokens
// follow the generic "split on _ / -, capitalize each word" rule.
//
// Empty string in → empty string out. The server emits non-empty keys
// in practice; the empty-string guard exists so this function is safe
// to call on adversarial input from a malformed wire body.
func TitleCaseLabel(key string) string {
	if key == "" {
		return ""
	}
	parts := splitOnUnderscoresOrHyphens(key)
	if len(parts) == 0 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, capitalizeWord(part))
	}
	return strings.Join(out, " ")
}

func splitOnUnderscoresOrHyphens(s string) []string {
	out := make([]string, 0, 4)
	start := 0
	for i, r := range s {
		if r == '_' || r == '-' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func capitalizeWord(word string) string {
	if word == "" {
		return ""
	}
	lower := strings.ToLower(word)
	if special, ok := specialLabels[lower]; ok {
		return special
	}
	runes := []rune(lower)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// CoercePlanInfo coerces a wire plan_info field into the typed array
// surface. Accepts both wire shapes:
//
//   - Post-zyins#349: []map[string]any with key/label/values — used verbatim.
//   - Pre-zyins#349: map[string]any with []string values — upconverted;
//     labels are Title Cased from each key so consumers see one shape only.
//
// Returns nil on any unrecognized shape — lenient by design so a
// forward-compatible field addition cannot break parsing.
func CoercePlanInfo(raw any) []PlanInfoItem {
	switch v := raw.(type) {
	case []any:
		return coerceTypedArray(v)
	case []map[string]any:
		return coerceTypedMaps(v)
	case map[string]any:
		return coerceLegacyMap(v)
	}
	return nil
}

func coerceTypedArray(entries []any) []PlanInfoItem {
	maps := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		maps = append(maps, entry)
	}
	return coerceTypedMaps(maps)
}

func coerceTypedMaps(entries []map[string]any) []PlanInfoItem {
	out := make([]PlanInfoItem, 0, len(entries))
	for _, entry := range entries {
		key, _ := entry["key"].(string)
		if key == "" {
			continue
		}
		labelRaw, _ := entry["label"].(string)
		label := labelRaw
		if label == "" {
			label = TitleCaseLabel(key)
		}
		out = append(out, PlanInfoItem{
			Key:    key,
			Label:  label,
			Values: coerceStringSlice(entry["values"]),
		})
	}
	return out
}

func coerceLegacyMap(m map[string]any) []PlanInfoItem {
	out := make([]PlanInfoItem, 0, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, PlanInfoItem{
			Key:    k,
			Label:  TitleCaseLabel(k),
			Values: coerceStringSlice(m[k]),
		})
	}
	return out
}

func coerceStringSlice(raw any) []string {
	switch arr := raw.(type) {
	case []string:
		return append([]string(nil), arr...)
	case []any:
		out := make([]string, 0, len(arr))
		for _, x := range arr {
			s, ok := x.(string)
			if !ok {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
