package reference

// CandidateConcept is the minimal information a [MatchAlgorithm] needs
// about each candidate. The fields are the lowest common denominator
// across the SDK's matcher surfaces — id + display name — so a custom
// algorithm can be plugged in without depending on the full Concept
// machinery.
type CandidateConcept struct {
	// ID is the catalog identifier (today equals the server-side
	// MakeKey-normalized form).
	ID string
	// Name is the display name from the catalog.
	Name string
	// Kind discriminates the candidate (medication / condition / ...).
	Kind ConceptKind
}

// MatchResult is returned by [MatchAlgorithm.Match]. A miss is encoded
// by Found=false; ID and Name are populated only when Found=true.
type MatchResult struct {
	// Found reports whether the input text matched a candidate.
	Found bool
	// Candidate echoes the matched candidate when Found=true. Zero
	// value when Found=false.
	Candidate CandidateConcept
}

// MatchAlgorithm resolves a free-text query against a candidate set.
// Implementations MUST be safe for concurrent use.
//
// The default implementation normalizes via MakeKey and looks up by
// exact key equality; alternative algorithms (fuzzy, edit-distance,
// embeddings) plug in by satisfying this interface and supplying it via
// [WithMatchAlgorithm] at the top-level SDK boundary.
type MatchAlgorithm interface {
	// Match returns the candidate whose normalized key matches the
	// query, or Found=false on a miss. Never returns an error — misses
	// are an expected outcome, not a failure mode.
	Match(query string, candidates []CandidateConcept) MatchResult
}

// MatchAlgorithmOption configures a [DefaultMatchAlgorithm].
type MatchAlgorithmOption func(*defaultMatchAlgorithmOptions)

// WithMatchAlgorithmVersionTag stamps the algorithm with a caller-
// supplied identifier reachable via VersionTag().
func WithMatchAlgorithmVersionTag(tag string) MatchAlgorithmOption {
	return func(o *defaultMatchAlgorithmOptions) {
		o.versionTag = tag
	}
}

type defaultMatchAlgorithmOptions struct {
	versionTag string
}

// DefaultMatchAlgorithm normalizes the query via MakeKey, then resolves
// against each candidate's MakeKey form. The lookup is O(n); callers
// that need O(1) lookups should use [Index.Medications] / [Index.Conditions]
// which prebuild an id-keyed index.
type DefaultMatchAlgorithm struct {
	versionTag string
}

// NewDefaultMatchAlgorithm constructs the default key-equality
// algorithm.
func NewDefaultMatchAlgorithm(opts ...MatchAlgorithmOption) *DefaultMatchAlgorithm {
	resolved := defaultMatchAlgorithmOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return &DefaultMatchAlgorithm{versionTag: resolved.versionTag}
}

// VersionTag returns the caller-supplied identifier.
func (a *DefaultMatchAlgorithm) VersionTag() string { return a.versionTag }

// Clone returns a new instance with the supplied options applied on
// top of this instance's configuration.
func (a *DefaultMatchAlgorithm) Clone(opts ...MatchAlgorithmOption) *DefaultMatchAlgorithm {
	resolved := defaultMatchAlgorithmOptions{versionTag: a.versionTag}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return &DefaultMatchAlgorithm{versionTag: resolved.versionTag}
}

// Match implements [MatchAlgorithm].
func (a *DefaultMatchAlgorithm) Match(query string, candidates []CandidateConcept) MatchResult {
	key := makeKey(query)
	if key == "" {
		return MatchResult{}
	}
	for _, c := range candidates {
		if makeKey(c.ID) == key {
			return MatchResult{Found: true, Candidate: c}
		}
	}
	// Second pass: match by display name. A candidate set sourced from
	// a non-id-normalized origin (e.g. raw spreadsheet) still resolves
	// if the display name normalizes to the same key.
	for _, c := range candidates {
		if makeKey(c.Name) == key {
			return MatchResult{Found: true, Candidate: c}
		}
	}
	return MatchResult{}
}
