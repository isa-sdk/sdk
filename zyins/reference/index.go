// Package reference — typed catalog access for the ISA zyins SDK.
//
// The reference package exposes a Concept handle for any medication,
// condition, or unknown free-text term. Symmetric accessors
// (Concept.Conditions, Concept.Medications) walk the inline-row v3
// catalog edges directly — no client-side key normalization, no
// client-side sort heuristics.
//
// Load-bearing invariants:
//   - makeKey is INTERNAL to Match. Consumers never compute keys
//     themselves.
//   - Match never returns an error. Unknown text returns a Concept with
//     IsKnown()==false, accessors return empty slices, and InputText()
//     preserves the original string.
//   - Lookups use indexes derived once from the inline-row edges at
//     bundle load time; the SDK does not re-derive on every Match call.
package reference

import (
	"sort"
	"sync"
)

// sortStable wraps sort.SliceStable so the index-permutation helpers
// below read top-down without a sort import on every call site.
func sortStable(idx []int, less func(i, j int) bool) {
	sort.SliceStable(idx, less)
}

// Entity is one typed row in a reference dataset (id + display name).
type Entity struct {
	ID   string
	Name string
}

// Relation is one inline row→row edge. FromID is the parent row's
// id; ToID is the related entity. PrescriptionCount is the observation
// count from the inline relation row; 0 means "no count emitted."
type Relation struct {
	FromID            string
	ToID              string
	ToName            string
	PrescriptionCount int
}

// DatasetsResponse is the minimal subset of the v3 datasets envelope
// the reference package needs.
//
// Conditions and Medications are the catalog rows. ConditionRelations
// contains every `treated_with` edge from a condition row;
// MedicationRelations contains every `used_for` edge from a medication
// row. The reference index derives reverse lookups internally.
type DatasetsResponse struct {
	Version             string
	Medications         []Entity
	Conditions          []Entity
	ConditionRelations  []Relation
	MedicationRelations []Relation
}

// Index is a read-only catalog wrapping one DatasetsResponse. It caches
// the derived reverse-index lazily and is safe for concurrent Match /
// traversal calls.
//
// Index is bound to a single dataset version. When the upstream version
// changes, consumers MUST rebuild via NewIndex — there is no mutation
// path.
type Index struct {
	version string

	Medications *MedicationsMatcher
	Conditions  *ConditionsMatcher
	Concepts    *ConceptsMatcher

	cat *catalog
}

// Version returns the dataset version this Index was built from. Use
// this to detect a stale cache and rebuild.
func (i *Index) Version() string { return i.version }

// NewIndex builds an Index over the supplied datasets envelope. The
// returned Index is immutable; rebuild on version change.
//
// NewIndex never returns an error: a nil or empty datasets argument
// yields an Index whose every Match call returns an unknown handle.
func NewIndex(datasets *DatasetsResponse) *Index {
	cat := buildCatalog(datasets)
	idx := &Index{cat: cat}
	if datasets != nil {
		idx.version = datasets.Version
	}
	idx.Medications = &MedicationsMatcher{cat: cat}
	idx.Conditions = &ConditionsMatcher{cat: cat}
	idx.Concepts = &ConceptsMatcher{cat: cat}
	return idx
}

// catalog is the internal read-only view backing all lookups.
type catalog struct {
	conditionNames  map[string]string
	medicationNames map[string]string
	conditionOrder  []string
	medicationOrder []string

	medicationsByCondition map[string][]string
	conditionsByMedication map[string][]string

	// freqCondForMedication[conditionID][medicationID] = inline
	// prescription_count from a condition row's treated_with edge.
	freqCondForMedication map[string]map[string]int
	// freqMedForCondition[medicationID][conditionID] = inline
	// prescription_count from a medication row's used_for edge.
	freqMedForCondition map[string]map[string]int

	reverseOnce sync.Once
}

func buildCatalog(d *DatasetsResponse) *catalog {
	if d == nil {
		return &catalog{}
	}
	c := &catalog{
		conditionNames:         make(map[string]string, len(d.Conditions)),
		medicationNames:        make(map[string]string, len(d.Medications)),
		conditionOrder:         make([]string, 0, len(d.Conditions)),
		medicationOrder:        make([]string, 0, len(d.Medications)),
		medicationsByCondition: make(map[string][]string),
		conditionsByMedication: make(map[string][]string),
		freqCondForMedication:  make(map[string]map[string]int),
		freqMedForCondition:    make(map[string]map[string]int),
	}
	for _, e := range d.Conditions {
		c.conditionNames[e.ID] = e.Name
		c.conditionOrder = append(c.conditionOrder, e.ID)
	}
	for _, e := range d.Medications {
		c.medicationNames[e.ID] = e.Name
		c.medicationOrder = append(c.medicationOrder, e.ID)
	}
	for _, r := range d.ConditionRelations {
		c.medicationsByCondition[r.FromID] = append(c.medicationsByCondition[r.FromID], r.ToID)
		if c.freqCondForMedication[r.FromID] == nil {
			c.freqCondForMedication[r.FromID] = make(map[string]int)
		}
		c.freqCondForMedication[r.FromID][r.ToID] = r.PrescriptionCount
	}
	for _, r := range d.MedicationRelations {
		c.conditionsByMedication[r.FromID] = append(c.conditionsByMedication[r.FromID], r.ToID)
		if c.freqMedForCondition[r.FromID] == nil {
			c.freqMedForCondition[r.FromID] = make(map[string]int)
		}
		c.freqMedForCondition[r.FromID][r.ToID] = r.PrescriptionCount
	}
	deriveMissingReverse(c)
	return c
}

// deriveMissingReverse fills in the reverse direction when only one
// side of the relationship was emitted on the wire (defensive against
// partial server payloads during the v3 cutover).
// deriveMissingReverse iterates conditionOrder / medicationOrder
// (catalog display order) so the reverse-index entries inherit a
// deterministic order under ties.
func deriveMissingReverse(c *catalog) {
	for _, condID := range c.conditionOrder {
		for _, medID := range c.medicationsByCondition[condID] {
			if !contains(c.conditionsByMedication[medID], condID) {
				c.conditionsByMedication[medID] = append(c.conditionsByMedication[medID], condID)
				if c.freqMedForCondition[medID] == nil {
					c.freqMedForCondition[medID] = make(map[string]int)
				}
				if _, has := c.freqMedForCondition[medID][condID]; !has {
					c.freqMedForCondition[medID][condID] = c.freqCondForMedication[condID][medID]
				}
			}
		}
	}
	for _, medID := range c.medicationOrder {
		for _, condID := range c.conditionsByMedication[medID] {
			if !contains(c.medicationsByCondition[condID], medID) {
				c.medicationsByCondition[condID] = append(c.medicationsByCondition[condID], medID)
				if c.freqCondForMedication[condID] == nil {
					c.freqCondForMedication[condID] = make(map[string]int)
				}
				if _, has := c.freqCondForMedication[condID][medID]; !has {
					c.freqCondForMedication[condID][medID] = c.freqMedForCondition[medID][condID]
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

func (c *catalog) conditionName(id string) (string, bool) {
	n, ok := c.conditionNames[id]
	return n, ok
}

func (c *catalog) medicationName(id string) (string, bool) {
	n, ok := c.medicationNames[id]
	return n, ok
}

func (c *catalog) medicationsForCondition(conditionID string) []string {
	return c.medicationsByCondition[conditionID]
}

func (c *catalog) conditionsForMedication(medicationID string) []string {
	return c.conditionsByMedication[medicationID]
}

// frequencyOfMedicationForCondition reads
// freqCondForMedication[conditionID][medicationID] — used to rank the
// medications inside a condition handle.
func (c *catalog) frequencyOfMedicationForCondition(conditionID, medicationID string) int {
	if row, ok := c.freqCondForMedication[conditionID]; ok {
		return row[medicationID]
	}
	return 0
}

// frequencyOfConditionForMedication reads
// freqMedForCondition[medicationID][conditionID] — used to rank the
// conditions inside a medication handle.
func (c *catalog) frequencyOfConditionForMedication(medicationID, conditionID string) int {
	if row, ok := c.freqMedForCondition[medicationID]; ok {
		return row[conditionID]
	}
	return 0
}

// orderMedicationIDsForCondition sorts medication ids by the requested
// Sort against the supplied condition.
func orderMedicationIDsForCondition(cat *catalog, ids []string, conditionID string, s Sort) []string {
	if s == SortAlphabetical {
		return sortByName(ids, func(id string) string {
			if n, ok := cat.medicationName(id); ok {
				return n
			}
			return id
		})
	}
	return sortByFrequency(ids, func(medID string) int {
		return cat.frequencyOfMedicationForCondition(conditionID, medID)
	})
}

// orderConditionIDsForMedication sorts condition ids by the requested
// Sort against the supplied medication.
func orderConditionIDsForMedication(cat *catalog, ids []string, medicationID string, s Sort) []string {
	if s == SortAlphabetical {
		return sortByName(ids, func(id string) string {
			if n, ok := cat.conditionName(id); ok {
				return n
			}
			return id
		})
	}
	return sortByFrequency(ids, func(condID string) int {
		return cat.frequencyOfConditionForMedication(medicationID, condID)
	})
}

// sortByFrequency returns ids ordered by descending frequency. Stable
// — ties preserve input order.
func sortByFrequency(ids []string, freq func(string) int) []string {
	idx := make([]int, len(ids))
	for i := range ids {
		idx[i] = i
	}
	sortStable(idx, func(i, j int) bool {
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
	sortStable(idx, func(i, j int) bool {
		return names[idx[i]] < names[idx[j]]
	})
	out := make([]string, len(ids))
	for i, k := range idx {
		out[i] = ids[k]
	}
	return out
}
