package reference

import (
	"context"
	"strings"
)

// AutocorrectMode governs typing-state heuristics applied by an
// [Autocorrector]. The two modes encode the locked-spec guards that
// keep mid-typing input from being prematurely "fixed" (keyup) and
// keep already-correct input from being doubled (submit).
type AutocorrectMode string

const (
	// AutocorrectModeKeyup guards against fixing partial typing:
	// "ASTHM" is left alone because "ASTHMA" contains it and is longer.
	AutocorrectModeKeyup AutocorrectMode = "keyup"
	// AutocorrectModeSubmit guards against duplication on commit:
	// "HIGH CHOLESTEROL" is left alone because the input already
	// contains the would-be correction "HIGH CHOLESTEROL".
	AutocorrectModeSubmit AutocorrectMode = "submit"
)

// CorrectOptions narrows the [Autocorrector.Correct] call.
type CorrectOptions struct {
	// Mode picks the typing-state guard. Default behavior in callers
	// that do not specify is [AutocorrectModeSubmit] — the conservative
	// choice for a finalize step.
	Mode AutocorrectMode
}

// AutocorrectEvent records one applied correction. Surfaces through
// the [DefaultAutocorrector] OnApplied callback.
type AutocorrectEvent struct {
	// Input is the verbatim text passed to Correct.
	Input string
	// Output is the corrected text returned to the caller.
	Output string
	// Mode is the [CorrectOptions] mode in effect.
	Mode AutocorrectMode
	// MatchedSpans lists the [start,end) word-index spans whose contents
	// the algorithm replaced. Indices reference the whitespace-tokenized
	// input. Empty when no correction was applied.
	MatchedSpans [][2]int
}

// Autocorrector applies typo corrections to free-text input.
// Implementations MUST be safe for concurrent use.
type Autocorrector interface {
	// Correct applies typo corrections per the supplied [CorrectOptions].
	// Returns the input verbatim on no match. The ctx is honored only by
	// implementations that perform I/O; the default implementation
	// ignores it.
	Correct(ctx context.Context, text string, opts CorrectOptions) string
}

// AutocorrectOnApplied is the callback signature for the
// [DefaultAutocorrector] OnApplied notification.
type AutocorrectOnApplied func(event AutocorrectEvent)

// AutocorrectorOption configures a [DefaultAutocorrector] at
// construction time. Mirrors the functional-options pattern used by
// [WithCaseStorage] and the zyins client builders.
type AutocorrectorOption func(*defaultAutocorrectorOptions)

// WithAutocorrectorVersionTag stamps the returned implementation with
// a caller-supplied version identifier reachable via VersionTag().
// Empty strings are accepted but discouraged.
func WithAutocorrectorVersionTag(tag string) AutocorrectorOption {
	return func(o *defaultAutocorrectorOptions) {
		o.versionTag = tag
	}
}

// WithAutocorrectorOnApplied registers a callback fired after every
// successful correction. The callback runs synchronously on the calling
// goroutine; long-running work MUST dispatch to a goroutine of its own.
func WithAutocorrectorOnApplied(cb AutocorrectOnApplied) AutocorrectorOption {
	return func(o *defaultAutocorrectorOptions) {
		o.onApplied = cb
	}
}

type defaultAutocorrectorOptions struct {
	versionTag string
	onApplied  AutocorrectOnApplied
}

// DefaultAutocorrector is the locked-spec n-gram window implementation.
// Construct via [NewDefaultAutocorrector]; the zero value is not usable.
type DefaultAutocorrector struct {
	typoMap    map[string]string
	versionTag string
	onApplied  AutocorrectOnApplied
}

// NewDefaultAutocorrector returns a [DefaultAutocorrector] keyed on
// the supplied uppercase typo map. The map is copied internally so the
// caller may mutate the original afterward; concurrent reads on the
// returned instance are safe.
//
// Algorithm mirrors bpp2.0 src/sah-ui/Input/TextField/useAutocorrect.js
// (the binding reference); see the spec at
// /tmp/v3-datasets-adapter-cutover-spec.md §2 for the contract.
func NewDefaultAutocorrector(typoMap map[string]string, opts ...AutocorrectorOption) *DefaultAutocorrector {
	resolved := defaultAutocorrectorOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	copied := make(map[string]string, len(typoMap))
	for k, v := range typoMap {
		copied[strings.ToUpper(k)] = v
	}
	return &DefaultAutocorrector{
		typoMap:    copied,
		versionTag: resolved.versionTag,
		onApplied:  resolved.onApplied,
	}
}

// VersionTag returns the caller-supplied version identifier set via
// [WithAutocorrectorVersionTag]; empty when unset.
func (a *DefaultAutocorrector) VersionTag() string { return a.versionTag }

// Clone returns a new [DefaultAutocorrector] with the supplied options
// applied on top of this instance's configuration. The typo map is
// shared with the source instance (read-only); option overrides replace
// the corresponding field on the clone.
func (a *DefaultAutocorrector) Clone(opts ...AutocorrectorOption) *DefaultAutocorrector {
	resolved := defaultAutocorrectorOptions{
		versionTag: a.versionTag,
		onApplied:  a.onApplied,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return &DefaultAutocorrector{
		typoMap:    a.typoMap,
		versionTag: resolved.versionTag,
		onApplied:  resolved.onApplied,
	}
}

// Correct applies the typo map to text using the n-gram window
// algorithm. See [Autocorrector] for the contract.
func (a *DefaultAutocorrector) Correct(_ context.Context, text string, opts CorrectOptions) string {
	if text == "" || len(a.typoMap) == 0 {
		return text
	}
	mode := opts.Mode
	if mode == "" {
		mode = AutocorrectModeSubmit
	}
	trailingWhitespace := ""
	if strings.HasSuffix(text, " ") {
		trailingWhitespace = " "
	}
	upper := strings.ToUpper(text)
	words := splitWords(upper)
	if len(words) == 0 {
		return text
	}
	result := make([]string, len(words))
	addedIndices := make(map[int]struct{}, len(words))
	var spans [][2]int

	// Mirror the JS double-loop: outer over window size minus one
	// (numWords), inner over start position.
	for numWords := 0; numWords < len(words); numWords++ {
		for i := 0; i < len(words); i++ {
			end := i + numWords + 1
			if end > len(words) {
				continue
			}
			windowWords := words[i:end]
			candidate := strings.Join(windowWords, " ")
			correction, ok := a.typoMap[candidate]
			if !ok {
				continue
			}
			if !shouldApplyCorrection(candidate, correction, upper, mode) {
				continue
			}
			result[i] = correction
			for n := 0; n <= numWords; n++ {
				if i+n < len(words) {
					addedIndices[i+n] = struct{}{}
				}
			}
			spans = append(spans, [2]int{i, end})
			numWords += len(windowWords) - 1
			break
		}
	}

	for i := 0; i < len(words); i++ {
		if result[i] != "" {
			continue
		}
		if _, marked := addedIndices[i]; marked {
			continue
		}
		result[i] = words[i]
	}

	out := strings.Join(compactStrings(result), " ") + trailingWhitespace
	if a.onApplied != nil && len(spans) > 0 {
		a.onApplied(AutocorrectEvent{
			Input:        text,
			Output:       out,
			Mode:         mode,
			MatchedSpans: spans,
		})
	}
	return out
}

// shouldApplyCorrection encodes the keyup vs submit guards.
//
//   - Keyup: skip when the correction CONTAINS the input AND is longer
//     than it. Prevents "ASTHM" → "ASTHMA" while still typing.
//   - Submit: skip when the input already CONTAINS the correction.
//     Prevents "HIGH CHOLESTEROL" → "HIGH HIGH CHOLESTEROL".
func shouldApplyCorrection(input, correction, upperHaystack string, mode AutocorrectMode) bool {
	upperCorrection := strings.ToUpper(correction)
	if mode == AutocorrectModeKeyup {
		if strings.Contains(upperCorrection, input) && len(upperCorrection) > len(input) {
			return false
		}
		return true
	}
	// Submit mode.
	return !strings.Contains(upperHaystack, upperCorrection)
}

// splitWords splits on any whitespace run, mirroring the JS regex
// /\s+/ tokenizer.
func splitWords(s string) []string {
	return strings.Fields(s)
}

// compactStrings drops empty entries while preserving order. Empty
// entries arise when the n-gram loop replaces a span with a single
// correction string at the head position.
func compactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
