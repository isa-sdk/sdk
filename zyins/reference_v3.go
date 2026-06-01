// Package zyins — v3 reference namespace.
//
// The reference namespace gives consumers a `Concept` handle for any
// medication, condition, or unknown free-text term. The v3 wire shape
// ships every relationship inline on the row (a condition row carries
// its `treated_with[]` medications + prescription counts; a medication
// row carries its `used_for[]` conditions); the SDK derives the lookup
// indexes once and serves the typed Concept handle to callers.
//
// Load-bearing invariants:
//   - makeKey is INTERNAL to Match. Consumers never compute keys
//     themselves.
//   - Match never returns an error. Unknown text returns a handle with
//     IsKnown()==false; accessors return empty slices; InputText() is
//     preserved.
//   - Lookups use the inline-row indexes derived once at bundle load
//     time; the SDK does not re-derive on every Match call.
//
// See packages/ts/src/zyins/reference.ts for the binding reference
// implementation and shared/schemas/sdk/testdata/reference_vectors.json
// for the cross-language parity corpus.

package zyins

import (
	"sort"
	"strings"
	"sync"
)

// ReferenceSort is the namespaced sort enum for Concept accessors.
type ReferenceSort string

const (
	// SortMostCommonFirst orders by descending prescription frequency
	// derived from the inline `treated_with` / `used_for` rows.
	SortMostCommonFirst ReferenceSort = "most_common_first"
	// SortAlphabetical orders by display name.
	SortAlphabetical ReferenceSort = "alphabetical"
)

// ConceptKind discriminates Concept handles.
type ConceptKind string

const (
	// ConceptKindMedication identifies a medication concept handle.
	ConceptKindMedication ConceptKind = "medication"
	// ConceptKindCondition identifies a condition concept handle.
	ConceptKindCondition ConceptKind = "condition"
	// ConceptKindUnknown identifies an unmatched free-text term.
	ConceptKindUnknown ConceptKind = "unknown"
)

// Concept is the read-only handle returned by Match.
type Concept interface {
	// ID is the opaque entity identifier. Empty when IsKnown() is false.
	ID() string
	// Name is the display name from the catalog; falls back to
	// InputText() when IsKnown() is false.
	Name() string
	// Kind discriminates this handle.
	Kind() ConceptKind
	// IsKnown reports whether the input text matched a known entity.
	IsKnown() bool
	// InputText is the verbatim text passed to Match.
	InputText() string
	// Conditions are the conditions a medication is used to treat.
	// Defined on medication handles; empty otherwise.
	Conditions(sort ReferenceSort) []Concept
	// Medications are the medications used to treat a condition.
	// Defined on condition handles; empty otherwise.
	Medications(sort ReferenceSort) []Concept
}

// ReferenceEntity is one typed entity row in the v3 catalog.
type ReferenceEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RelationEdge is one inline row→row relationship in the v3 catalog.
// FromID points at the row's own ID (the parent condition or
// medication); ToID points at the related entity. ToName is the
// related entity's display name as it appears in the parent's inline
// list. PrescriptionCount is the inline observation count used to
// drive SortMostCommonFirst — equal to 0 when the wire row did not
// carry one.
type RelationEdge struct {
	FromID            string
	ToID              string
	ToName            string
	PrescriptionCount int
}

// NicotineOption is one row in the v3 nicotine_options dataset.
type NicotineOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type discriminates nicotine usage modes (smoked, smokeless, etc).
	Type string `json:"type"`
}

// SpellingCorrection is one row in the v3 spelling_corrections dataset.
// The From → To mapping is what feeds the autocorrector typoMap.
type SpellingCorrection struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

// DatasetEntry is one named dataset within the catalog.
type DatasetEntry struct {
	Version   string            `json:"version"`
	ItemCount int               `json:"item_count"`
	Items     []ReferenceEntity `json:"items,omitempty"`
}

// DatasetCategory is the closed enum of dataset categories the server
// returns. Mirrors the wire enum.
type DatasetCategory string

const (
	// DatasetCategoryMedications is the medications dataset.
	DatasetCategoryMedications DatasetCategory = "medications"
	// DatasetCategoryConditions is the conditions dataset.
	DatasetCategoryConditions DatasetCategory = "conditions"
	// DatasetCategoryProducts is the products dataset.
	DatasetCategoryProducts DatasetCategory = "products"
	// DatasetCategoryCorrections is the legacy alias for spelling
	// corrections. The canonical category is spelling_corrections; the
	// SDK accepts both during the rc.1 → 1.0 cutover.
	DatasetCategoryCorrections DatasetCategory = "corrections"
	// DatasetCategoryNicotineOptions is the nicotine_options dataset.
	DatasetCategoryNicotineOptions DatasetCategory = "nicotine_options"
)

// datasetCategorySpellingCorrections is the new canonical
// spelling_corrections category. Unexported because callers always
// reach corrections via the typed bundle.SpellingCorrections accessor.
const datasetCategorySpellingCorrections DatasetCategory = "spelling_corrections"

// DatasetBundleV3 is the standalone-licensable v3 catalog. Every
// relationship lives inline on the entity row; the SDK derives the
// id-keyed lookup indexes once at first use.
type DatasetBundleV3 struct {
	// ETag for conditional revalidation; empty when no header present.
	ETag string `json:"-"`
	// Version is the bundle's catalog_version (or the legacy `version`
	// root field if that's all the server emitted).
	Version string `json:"version"`
	// Medications is the medications dataset rows.
	Medications []ReferenceEntity `json:"-"`
	// Conditions is the conditions dataset rows.
	Conditions []ReferenceEntity `json:"-"`
	// Products is the products dataset rows (reserved; not used by the
	// reference matcher today).
	Products []ReferenceEntity `json:"-"`
	// NicotineOptions is the nicotine_options dataset.
	NicotineOptions []NicotineOption `json:"-"`
	// SpellingCorrections is the spelling_corrections dataset.
	// Consumers either pass it to NewDefaultAutocorrector to build a
	// typo map, or get the pre-bound corrector via the SDK facade.
	SpellingCorrections []SpellingCorrection `json:"-"`
	// ConditionRelations is the inline-derived condition →
	// medication edges (one RelationEdge per `treated_with` entry).
	ConditionRelations []RelationEdge `json:"-"`
	// MedicationRelations is the inline-derived medication →
	// condition edges (one RelationEdge per `used_for` entry).
	MedicationRelations []RelationEdge `json:"-"`
	// Datasets is the per-category metadata map (version + item_count
	// + items) preserved from the wire response.
	Datasets map[DatasetCategory]*DatasetEntry `json:"datasets"`
	// ProductsByFamily is the product slice keyed by family slug — the
	// products available within each marketing family. Empty when the
	// server omits the slice. Consumers read this directly rather than
	// re-deriving family membership from flat product rows.
	ProductsByFamily map[string][]ReferenceEntity `json:"-"`
	// DiscontinuedProducts maps a product slug → the unix epoch second at
	// which the product was discontinued. Empty when none / omitted.
	//
	// The value is int64 (not int) because unix epoch seconds overflow a
	// 32-bit int after 2038-01-19, and Go's int is 32-bit on 32-bit
	// targets; the C#/TS/Python/PHP mirrors all hold 64-bit values here.
	DiscontinuedProducts map[string]int64 `json:"-"`
	// StateDerivatives lists state slugs whose product availability
	// derives from another state's ruleset. Empty when omitted.
	StateDerivatives []string `json:"-"`

	catalogOnce sync.Once
	catalog     *refCatalog
}

// SpellingTypoMap returns the From → To map suitable for
// NewDefaultAutocorrector. Keys are uppercased; values are passed
// through verbatim.
func (b *DatasetBundleV3) SpellingTypoMap() map[string]string {
	if b == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(b.SpellingCorrections))
	for _, c := range b.SpellingCorrections {
		out[strings.ToUpper(c.From)] = c.To
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal — make_key normalizer (the only place a key is derived).
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Catalog facade — the read-only view backing every matcher.
// ---------------------------------------------------------------------------

type refCatalog struct {
	conditionNames  map[string]string
	medicationNames map[string]string
	conditionOrder  []string
	medicationOrder []string

	// medicationsByCondition: conditionID → ordered list of medicationIDs
	// (server-emitted ordering, which is descending by prescription_count
	// per spec §1).
	medicationsByCondition map[string][]string
	// conditionsByMedication: medicationID → ordered list of conditionIDs.
	conditionsByMedication map[string][]string

	// freqMedForCondition[medicationID][conditionID] = prescription_count
	// from the medication row's `used_for` inline list. Used by Concept
	// (Medication).Conditions(SortMostCommonFirst).
	freqMedForCondition map[string]map[string]int
	// freqCondForMedication[conditionID][medicationID] = prescription_count
	// from the condition row's `treated_with` inline list. Used by
	// Concept(Condition).Medications(SortMostCommonFirst).
	freqCondForMedication map[string]map[string]int
}

func buildCatalog(bundle *DatasetBundleV3) *refCatalog {
	cat := &refCatalog{
		conditionNames:         make(map[string]string),
		medicationNames:        make(map[string]string),
		medicationsByCondition: make(map[string][]string),
		conditionsByMedication: make(map[string][]string),
		freqMedForCondition:    make(map[string]map[string]int),
		freqCondForMedication:  make(map[string]map[string]int),
	}
	for _, e := range bundle.Conditions {
		cat.conditionNames[e.ID] = e.Name
		cat.conditionOrder = append(cat.conditionOrder, e.ID)
	}
	for _, e := range bundle.Medications {
		cat.medicationNames[e.ID] = e.Name
		cat.medicationOrder = append(cat.medicationOrder, e.ID)
	}
	// Condition rows publish their inline treated_with[] medications.
	for _, edge := range bundle.ConditionRelations {
		cat.medicationsByCondition[edge.FromID] = append(cat.medicationsByCondition[edge.FromID], edge.ToID)
		if cat.freqCondForMedication[edge.FromID] == nil {
			cat.freqCondForMedication[edge.FromID] = make(map[string]int)
		}
		cat.freqCondForMedication[edge.FromID][edge.ToID] = edge.PrescriptionCount
	}
	// Medication rows publish their inline used_for[] conditions.
	for _, edge := range bundle.MedicationRelations {
		cat.conditionsByMedication[edge.FromID] = append(cat.conditionsByMedication[edge.FromID], edge.ToID)
		if cat.freqMedForCondition[edge.FromID] == nil {
			cat.freqMedForCondition[edge.FromID] = make(map[string]int)
		}
		cat.freqMedForCondition[edge.FromID][edge.ToID] = edge.PrescriptionCount
	}
	// Fallback: if a relationship was published from only one side
	// (condition→medication but not medication→condition, or vice
	// versa) — derive the reverse from what's available. Defensive
	// against partial server payloads during the cutover.
	deriveMissingReverse(cat)
	return cat
}

// deriveMissingReverse iterates the catalog (display) order so the
// reverse-index entries inherit a deterministic order under ties.
func deriveMissingReverse(cat *refCatalog) {
	for _, condID := range cat.conditionOrder {
		for _, medID := range cat.medicationsByCondition[condID] {
			if !contains(cat.conditionsByMedication[medID], condID) {
				cat.conditionsByMedication[medID] = append(cat.conditionsByMedication[medID], condID)
				if cat.freqMedForCondition[medID] == nil {
					cat.freqMedForCondition[medID] = make(map[string]int)
				}
				if _, has := cat.freqMedForCondition[medID][condID]; !has {
					cat.freqMedForCondition[medID][condID] = cat.freqCondForMedication[condID][medID]
				}
			}
		}
	}
	for _, medID := range cat.medicationOrder {
		for _, condID := range cat.conditionsByMedication[medID] {
			if !contains(cat.medicationsByCondition[condID], medID) {
				cat.medicationsByCondition[condID] = append(cat.medicationsByCondition[condID], medID)
				if cat.freqCondForMedication[condID] == nil {
					cat.freqCondForMedication[condID] = make(map[string]int)
				}
				if _, has := cat.freqCondForMedication[condID][medID]; !has {
					cat.freqCondForMedication[condID][medID] = cat.freqMedForCondition[medID][condID]
				}
			}
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func (c *refCatalog) conditionName(id string) (string, bool) {
	n, ok := c.conditionNames[id]
	return n, ok
}
func (c *refCatalog) medicationName(id string) (string, bool) {
	n, ok := c.medicationNames[id]
	return n, ok
}
func (c *refCatalog) medicationsForCondition(conditionID string) []string {
	return c.medicationsByCondition[conditionID]
}
func (c *refCatalog) conditionsForMedication(medicationID string) []string {
	return c.conditionsByMedication[medicationID]
}

// frequencyOfMedicationForCondition reads the inline count for
// "how often this medication treats that condition" — used to rank
// medications inside a condition handle.
func (c *refCatalog) frequencyOfMedicationForCondition(conditionID, medicationID string) int {
	if row, ok := c.freqCondForMedication[conditionID]; ok {
		return row[medicationID]
	}
	return 0
}

// frequencyOfConditionForMedication reads "how often this medication
// is used for that condition" — used to rank conditions inside a
// medication handle.
func (c *refCatalog) frequencyOfConditionForMedication(medicationID, conditionID string) int {
	if row, ok := c.freqMedForCondition[medicationID]; ok {
		return row[conditionID]
	}
	return 0
}

func catalogFor(bundle *DatasetBundleV3) *refCatalog {
	if bundle == nil {
		return &refCatalog{}
	}
	bundle.catalogOnce.Do(func() {
		bundle.catalog = buildCatalog(bundle)
	})
	return bundle.catalog
}

// ---------------------------------------------------------------------------
// Concept implementation.
// ---------------------------------------------------------------------------

type concept struct {
	id         string
	name       string
	kind       ConceptKind
	isKnown    bool
	inputText  string
	refCatalog *refCatalog
}

func (c *concept) ID() string        { return c.id }
func (c *concept) Name() string      { return c.name }
func (c *concept) Kind() ConceptKind { return c.kind }
func (c *concept) IsKnown() bool     { return c.isKnown }
func (c *concept) InputText() string { return c.inputText }

func (c *concept) Conditions(s ReferenceSort) []Concept {
	if c.kind != ConceptKindMedication || c.refCatalog == nil {
		return []Concept{}
	}
	conditionIDs := c.refCatalog.conditionsForMedication(c.id)
	var ordered []string
	if s == SortAlphabetical {
		ordered = sortByName(conditionIDs, func(id string) string {
			if n, ok := c.refCatalog.conditionName(id); ok {
				return n
			}
			return id
		})
	} else {
		ordered = sortByFrequency(conditionIDs, func(condID string) int {
			return c.refCatalog.frequencyOfConditionForMedication(c.id, condID)
		})
	}
	out := make([]Concept, 0, len(ordered))
	for _, condID := range ordered {
		out = append(out, buildConditionConcept(c.refCatalog, condID, c.inputText))
	}
	return out
}

func (c *concept) Medications(s ReferenceSort) []Concept {
	if c.kind != ConceptKindCondition || c.refCatalog == nil {
		return []Concept{}
	}
	medIDs := c.refCatalog.medicationsForCondition(c.id)
	var ordered []string
	if s == SortAlphabetical {
		ordered = sortByName(medIDs, func(id string) string {
			if n, ok := c.refCatalog.medicationName(id); ok {
				return n
			}
			return id
		})
	} else {
		ordered = sortByFrequency(medIDs, func(medID string) int {
			return c.refCatalog.frequencyOfMedicationForCondition(c.id, medID)
		})
	}
	out := make([]Concept, 0, len(ordered))
	for _, medID := range ordered {
		out = append(out, buildMedicationConcept(c.refCatalog, medID, c.inputText))
	}
	return out
}

func sortByFrequency(ids []string, freq func(string) int) []string {
	idx := make([]int, len(ids))
	for i := range ids {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		return freq(ids[idx[i]]) > freq(ids[idx[j]])
	})
	out := make([]string, len(ids))
	for i, k := range idx {
		out[i] = ids[k]
	}
	return out
}

func sortByName(ids []string, nameOf func(string) string) []string {
	idx := make([]int, len(ids))
	for i := range ids {
		idx[i] = i
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = nameOf(id)
	}
	sort.SliceStable(idx, func(i, j int) bool {
		return names[idx[i]] < names[idx[j]]
	})
	out := make([]string, len(ids))
	for i, k := range idx {
		out[i] = ids[k]
	}
	return out
}

func buildMedicationConcept(cat *refCatalog, id, inputText string) *concept {
	name, _ := cat.medicationName(id)
	if name == "" {
		name = inputText
	}
	return &concept{
		id:         id,
		name:       name,
		kind:       ConceptKindMedication,
		isKnown:    true,
		inputText:  inputText,
		refCatalog: cat,
	}
}

func buildConditionConcept(cat *refCatalog, id, inputText string) *concept {
	name, _ := cat.conditionName(id)
	if name == "" {
		name = inputText
	}
	return &concept{
		id:         id,
		name:       name,
		kind:       ConceptKindCondition,
		isKnown:    true,
		inputText:  inputText,
		refCatalog: cat,
	}
}

func buildUnknownConcept(inputText string) *concept {
	return &concept{
		id:        "",
		name:      inputText,
		kind:      ConceptKindUnknown,
		isKnown:   false,
		inputText: inputText,
	}
}

// ---------------------------------------------------------------------------
// Public matchers — bundle-required surface.
// ---------------------------------------------------------------------------

// ReferenceMatcher resolves free text into a Concept against an
// explicit *DatasetBundleV3. The Client.Medications / Conditions /
// Concepts top-level surface is the cache-backed sugar over this.
type ReferenceMatcher interface {
	Match(text string, bundle *DatasetBundleV3) Concept
}

// ReferenceService is the stateless bundle-required matcher surface.
type ReferenceService struct {
	medications medicationMatcher
	conditions  conditionMatcher
	concepts    conceptMatcher
}

// Medications returns the matcher that resolves free text against the
// medication catalog.
func (s *ReferenceService) Medications() ReferenceMatcher { return s.medications }

// Conditions returns the matcher that resolves free text against the
// condition catalog.
func (s *ReferenceService) Conditions() ReferenceMatcher { return s.conditions }

// Concepts returns the kind-agnostic matcher.
func (s *ReferenceService) Concepts() ReferenceMatcher { return s.concepts }

func newReferenceService() *ReferenceService { return &ReferenceService{} }

type medicationMatcher struct{}
type conditionMatcher struct{}
type conceptMatcher struct{}

func (medicationMatcher) Match(text string, bundle *DatasetBundleV3) Concept {
	cat := catalogFor(bundle)
	key := makeKey(text)
	if key != "" {
		if _, ok := cat.medicationName(key); ok {
			return buildMedicationConcept(cat, key, text)
		}
	}
	return buildUnknownConcept(text)
}

func (conditionMatcher) Match(text string, bundle *DatasetBundleV3) Concept {
	cat := catalogFor(bundle)
	key := makeKey(text)
	if key != "" {
		if _, ok := cat.conditionName(key); ok {
			return buildConditionConcept(cat, key, text)
		}
	}
	return buildUnknownConcept(text)
}

func (conceptMatcher) Match(text string, bundle *DatasetBundleV3) Concept {
	cat := catalogFor(bundle)
	key := makeKey(text)
	if key == "" {
		return buildUnknownConcept(text)
	}
	if _, ok := cat.conditionName(key); ok {
		return buildConditionConcept(cat, key, text)
	}
	if _, ok := cat.medicationName(key); ok {
		return buildMedicationConcept(cat, key, text)
	}
	return buildUnknownConcept(text)
}
