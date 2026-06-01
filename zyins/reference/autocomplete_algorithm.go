package reference

import (
	"context"
	"sort"
	"strings"
)

// AutocompleteOptions narrows an [AutocompleteAlgorithm.Rank] call.
type AutocompleteOptions struct {
	// Limit caps the returned suggestion count. <= 0 means no cap.
	Limit int
	// Kinds restricts candidates to the supplied [ConceptKind] set;
	// empty means accept every kind.
	Kinds []ConceptKind
	// Frequencies maps candidate ID → prescription_count (or any
	// monotonic popularity signal). Missing entries score as 0; an
	// entirely empty/missing map disables the frequency boost.
	Frequencies map[string]int
	// Sort selects the result ordering. The zero value
	// (SortMostCommonFirst) keeps the bucketed relevance + frequency
	// boost order. SortAlphabetical keeps the same relevance FILTER —
	// only matching candidates are returned — but emits them in a flat
	// case-insensitive A→Z order by display name, for an A-Z toggle in a
	// narrowing UI.
	Sort Sort
}

// AutocompleteAlgorithm ranks candidates against a free-text query.
// Implementations MUST be safe for concurrent use.
type AutocompleteAlgorithm interface {
	// Rank returns suggestions in descending score order. The ctx is
	// honored only by implementations that perform I/O; the default
	// implementation ignores it. Never returns an error — ranking is
	// pure CPU.
	Rank(ctx context.Context, query string, candidates []CandidateConcept, opts AutocompleteOptions) []Suggestion
}

// AutocompleteAlgorithmOption configures a
// [DefaultAutocompleteAlgorithm].
type AutocompleteAlgorithmOption func(*defaultAutocompleteAlgorithmOptions)

// WithAutocompleteAlgorithmVersionTag stamps the algorithm with a
// caller-supplied identifier reachable via VersionTag().
func WithAutocompleteAlgorithmVersionTag(tag string) AutocompleteAlgorithmOption {
	return func(o *defaultAutocompleteAlgorithmOptions) {
		o.versionTag = tag
	}
}

type defaultAutocompleteAlgorithmOptions struct {
	versionTag string
}

// DefaultAutocompleteAlgorithm is the locked-spec bucketed algorithm.
// See bpp2.0 src/sah-ui/Input/TextField/useAutocomplete.js for the
// binding reference; the bucket priorities and frequency-boost formula
// are defined in /tmp/v3-datasets-adapter-cutover-spec.md §2.
type DefaultAutocompleteAlgorithm struct {
	versionTag string
}

// NewDefaultAutocompleteAlgorithm constructs the default bucketed
// ranker.
func NewDefaultAutocompleteAlgorithm(opts ...AutocompleteAlgorithmOption) *DefaultAutocompleteAlgorithm {
	resolved := defaultAutocompleteAlgorithmOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return &DefaultAutocompleteAlgorithm{versionTag: resolved.versionTag}
}

// VersionTag returns the caller-supplied identifier.
func (a *DefaultAutocompleteAlgorithm) VersionTag() string { return a.versionTag }

// Clone returns a new instance with the supplied options applied.
func (a *DefaultAutocompleteAlgorithm) Clone(opts ...AutocompleteAlgorithmOption) *DefaultAutocompleteAlgorithm {
	resolved := defaultAutocompleteAlgorithmOptions{versionTag: a.versionTag}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return &DefaultAutocompleteAlgorithm{versionTag: resolved.versionTag}
}

// Rank implements [AutocompleteAlgorithm].
func (a *DefaultAutocompleteAlgorithm) Rank(_ context.Context, query string, candidates []CandidateConcept, opts AutocompleteOptions) []Suggestion {
	if query == "" || len(candidates) == 0 {
		return nil
	}
	filtered := filterCandidatesByKind(candidates, opts.Kinds)
	if len(filtered) == 0 {
		return nil
	}
	wordsInInput := tokenizeAutocomplete(query)
	upperInput := strings.ToUpper(query)

	prefilter := prefilterByWordOverlap(filtered, wordsInInput, upperInput)
	buckets := bucketize(prefilter, wordsInInput, upperInput)
	grouped := groupedFromBuckets(buckets)

	// Order within the matched set. SortAlphabetical flattens every bucket
	// into one A→Z group (the relevance filter already decided membership);
	// the default boosts by frequency within each bucket.
	var flat []CandidateConcept
	if opts.Sort == SortAlphabetical {
		grouped = [][]CandidateConcept{flattenAlphabetical(grouped)}
		flat = grouped[0]
	} else {
		grouped = applyFrequencyBoost(grouped, opts.Frequencies)
		flat = flattenDedupe(grouped)
	}
	if opts.Limit > 0 && len(flat) > opts.Limit {
		flat = flat[:opts.Limit]
	}
	// Score is the bucket-boosted (frequency+1)*scale value — NOT the result
	// position — for both sort modes, matching the TS/Python/PHP reference
	// parsers. Alphabetical collapses to a single group (scale 1), so every
	// suggestion scores frequency+1; the default mode's per-group scale
	// preserves bucket priority.
	scoreOf := computeAutocompleteScoreLookup(grouped, opts.Frequencies)
	out := make([]Suggestion, len(flat))
	for i, c := range flat {
		out[i] = Suggestion{
			Concept: c,
			Score:   float64(scoreOf[autocompleteScoreKey(c)]),
			Rank:    i,
		}
	}
	return out
}

// ---------------------------------------------------------------------
// Internals — direct ports of the JS worker logic.
// ---------------------------------------------------------------------

func filterCandidatesByKind(candidates []CandidateConcept, kinds []ConceptKind) []CandidateConcept {
	if len(kinds) == 0 {
		return candidates
	}
	allowed := make(map[ConceptKind]struct{}, len(kinds))
	for _, k := range kinds {
		allowed[k] = struct{}{}
	}
	out := candidates[:0:0]
	for _, c := range candidates {
		if _, ok := allowed[c.Kind]; ok {
			out = append(out, c)
		}
	}
	return out
}

// tokenizeAutocomplete mirrors the JS tokenizeString: uppercase, split
// on whitespace, strip non-alphanumerics per word, drop empties.
func tokenizeAutocomplete(s string) []string {
	upper := strings.ToUpper(s)
	parts := strings.Fields(upper)
	out := parts[:0]
	for _, p := range parts {
		var b strings.Builder
		b.Grow(len(p))
		for i := 0; i < len(p); i++ {
			ch := p[i]
			if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
				b.WriteByte(ch)
			}
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	return out
}

// prefilterByWordOverlap mirrors the JS filterOptions function.
func prefilterByWordOverlap(candidates []CandidateConcept, wordsInInput []string, upperInput string) []CandidateConcept {
	if len(wordsInInput) < 2 {
		cleaned := strings.ReplaceAll(upperInput, "(", "")
		out := candidates[:0:0]
		for _, c := range candidates {
			name := strings.ToUpper(strings.ReplaceAll(c.Name, "(", ""))
			if strings.Contains(name, cleaned) {
				out = append(out, c)
			}
		}
		return out
	}
	out := candidates[:0:0]
	for _, c := range candidates {
		name := strings.ToUpper(strings.ReplaceAll(c.Name, "(", ""))
		matches := 0
		for _, w := range wordsInInput {
			if strings.Contains(name, w) {
				matches++
			}
		}
		if len(wordsInInput)-matches <= 1 {
			out = append(out, c)
		}
	}
	return out
}

type bucketSet struct {
	startsWith                  []CandidateConcept
	sameWords                   []CandidateConcept
	independentWordIntersection []CandidateConcept
	wordCountNoTolerance        map[int][]CandidateConcept
	sameNumWithTolerance        []CandidateConcept
	wordCountWithTolerance      map[int][]CandidateConcept
}

func bucketize(candidates []CandidateConcept, wordsInInput []string, upperInput string) bucketSet {
	bs := bucketSet{
		wordCountNoTolerance:   map[int][]CandidateConcept{},
		wordCountWithTolerance: map[int][]CandidateConcept{},
	}
	setIn := make(map[string]struct{}, len(wordsInInput))
	for _, w := range wordsInInput {
		setIn[w] = struct{}{}
	}
	cleanedInput := strings.ReplaceAll(upperInput, "(", "")

	for _, c := range candidates {
		cleaned := strings.ReplaceAll(c.Name, "(", "")
		upperCleaned := strings.ToUpper(cleaned)
		wordsInOption := tokenizeAutocomplete(cleaned)
		setOpt := make(map[string]struct{}, len(wordsInOption))
		for _, w := range wordsInOption {
			setOpt[w] = struct{}{}
		}
		isStartMatch := strings.HasPrefix(upperCleaned, cleanedInput)
		isSameLength := len(wordsInOption) == len(wordsInInput)
		lengthDiff := absInt(len(wordsInInput) - len(wordsInOption))
		wordsInOptionIsSupersetOfInput := allWordsContained(wordsInInput, setOpt)
		// independentWordIntersection: every input word appears as a substring
		// of the option, but the option is NOT a superset of the input token
		// set. Mirrors the locked TS `independentWordIntersection` guard.
		isIndependentIntersection := !wordsInOptionIsSupersetOfInput &&
			everyInputWordInOptionString(wordsInInput, cleaned)

		// Mutually exclusive buckets in the locked TS precedence order:
		// startsWith > sameWords > independentWordIntersection >
		// wordCountNoTolerance > sameNumWithTolerance > wordCountWithTolerance.
		// Each candidate lands in exactly ONE bucket (the JS uses an else-if
		// chain); an additive assignment double-counts and lets a superset
		// outrank an independent match via the within-bucket frequency boost.
		switch {
		case isStartMatch:
			bs.startsWith = append(bs.startsWith, c)
		case isSameLength && setsEqual(setIn, setOpt):
			bs.sameWords = append(bs.sameWords, c)
		case isIndependentIntersection:
			bs.independentWordIntersection = append(bs.independentWordIntersection, c)
		case wordsInOptionIsSupersetOfInput:
			bs.wordCountNoTolerance[lengthDiff] = append(bs.wordCountNoTolerance[lengthDiff], c)
		case isSameLength:
			bs.sameNumWithTolerance = append(bs.sameNumWithTolerance, c)
		default:
			bs.wordCountWithTolerance[lengthDiff] = append(bs.wordCountWithTolerance[lengthDiff], c)
		}
	}
	// startsWith sorts ascending by word count, mirroring the JS.
	sort.SliceStable(bs.startsWith, func(i, j int) bool {
		return wordCountOf(bs.startsWith[i].Name) < wordCountOf(bs.startsWith[j].Name)
	})
	return bs
}

func groupedFromBuckets(bs bucketSet) [][]CandidateConcept {
	wordCountKeysNoTol := sortedIntKeys(bs.wordCountNoTolerance)
	wordCountOptionsNoTol := flatByKeys(bs.wordCountNoTolerance, wordCountKeysNoTol)
	wordCountKeysWithTol := sortedIntKeys(bs.wordCountWithTolerance)
	wordCountOptionsWithTol := flatByKeys(bs.wordCountWithTolerance, wordCountKeysWithTol)
	// Bucket priority order mirrors the locked TS/JS reference:
	// startsWith > sameWords > independentWordIntersection >
	// wordCountNoTolerance > sameNumWithTolerance > wordCountWithTolerance.
	// independentWordIntersection ranks ABOVE wordCountNoTolerance.
	return [][]CandidateConcept{
		bs.startsWith,
		bs.sameWords,
		bs.independentWordIntersection,
		wordCountOptionsNoTol,
		bs.sameNumWithTolerance,
		wordCountOptionsWithTol,
	}
}

// applyFrequencyBoost mirrors the JS applyFrequencySorting. Within
// each bucket, sort candidates by (frequency+1)*scaleFactor descending.
// Skip entirely if no candidate has a frequency entry.
func applyFrequencyBoost(grouped [][]CandidateConcept, frequencies map[string]int) [][]CandidateConcept {
	if len(frequencies) == 0 {
		return grouped
	}
	foundSomething := false
	maxScaleFactor := len(grouped)
	scoreOf := make(map[string]int)

	for groupIndex, group := range grouped {
		scaleFactor := maxScaleFactor - groupIndex
		if scaleFactor < 1 {
			scaleFactor = 1
		}
		for _, c := range group {
			freq := frequencies[c.ID]
			if freq > 0 {
				foundSomething = true
			}
			scoreOf[c.ID] = (freq + 1) * scaleFactor
		}
	}
	if !foundSomething {
		return grouped
	}
	out := make([][]CandidateConcept, len(grouped))
	for i, group := range grouped {
		copied := append([]CandidateConcept(nil), group...)
		sort.SliceStable(copied, func(a, b int) bool {
			sa := scoreOf[copied[a].ID]
			sb := scoreOf[copied[b].ID]
			if sa != sb {
				return sa > sb
			}
			return copied[a].Name < copied[b].Name
		})
		out[i] = copied
	}
	return out
}

// autocompleteScoreKey is the per-candidate key for the score lookup. It
// mirrors the TS reference (id when present; a name-derived fallback when the
// id is empty) so the same candidate scores identically across SDKs.
func autocompleteScoreKey(c CandidateConcept) string {
	if c.ID != "" {
		return c.ID
	}
	return "__unknown\x00" + c.Name
}

// computeAutocompleteScoreLookup assigns each candidate its bucket-boosted
// score (frequency+1)*scale, where scale = max(1, total-groupIndex) and
// total is the number of groups. First occurrence of a key wins. This is the
// score consumers see on a Suggestion — a direct port of the TS/Python
// computeScoreLookup, so a single A→Z group yields frequency+1 and the
// default mode's earlier buckets score higher.
func computeAutocompleteScoreLookup(grouped [][]CandidateConcept, frequencies map[string]int) map[string]int {
	total := len(grouped)
	out := make(map[string]int)
	for groupIndex, group := range grouped {
		scale := total - groupIndex
		if scale < 1 {
			scale = 1
		}
		for _, c := range group {
			key := autocompleteScoreKey(c)
			if _, ok := out[key]; ok {
				continue
			}
			freq := 0
			if c.ID != "" {
				freq = frequencies[c.ID]
			}
			out[key] = (freq + 1) * scale
		}
	}
	return out
}

func flattenDedupe(grouped [][]CandidateConcept) []CandidateConcept {
	seen := make(map[string]struct{})
	out := make([]CandidateConcept, 0)
	for _, group := range grouped {
		for _, c := range group {
			key := string(c.Kind) + "\x00" + c.ID + "\x00" + c.Name
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, c)
		}
	}
	return out
}

// flattenAlphabetical collapses every relevance bucket into one
// case-insensitive A→Z group. De-dupes by the same key as flattenDedupe
// (first occurrence across buckets wins before the sort) so a candidate
// appearing in two buckets is not double-listed. Ties break by
// case-sensitive name then id for stable, cross-language output.
func flattenAlphabetical(grouped [][]CandidateConcept) []CandidateConcept {
	flat := flattenDedupe(grouped)
	sort.SliceStable(flat, func(i, j int) bool {
		li, lj := strings.ToLower(flat[i].Name), strings.ToLower(flat[j].Name)
		if li != lj {
			return li < lj
		}
		if flat[i].Name != flat[j].Name {
			return flat[i].Name < flat[j].Name
		}
		return flat[i].ID < flat[j].ID
	})
	return flat
}

// ---------------------------------------------------------------------
// Tiny helpers — extracted to keep the algorithm body legible.
// ---------------------------------------------------------------------

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func allWordsContained(words []string, set map[string]struct{}) bool {
	for _, w := range words {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}

func setsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func everyInputWordInOptionString(words []string, optionStr string) bool {
	upper := strings.ToUpper(optionStr)
	for _, w := range words {
		if !strings.Contains(upper, w) {
			return false
		}
	}
	return true
}

func wordCountOf(name string) int {
	return len(strings.Fields(name))
}

func sortedIntKeys(m map[int][]CandidateConcept) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func flatByKeys(m map[int][]CandidateConcept, keys []int) []CandidateConcept {
	out := make([]CandidateConcept, 0)
	for _, k := range keys {
		out = append(out, m[k]...)
	}
	return out
}
