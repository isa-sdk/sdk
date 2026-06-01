package reference

// ConceptKind discriminates a Concept handle.
type ConceptKind string

const (
	// KindMedication identifies a medication concept handle.
	KindMedication ConceptKind = "medication"
	// KindCondition identifies a condition concept handle.
	KindCondition ConceptKind = "condition"
	// KindUnknown identifies an unmatched free-text term. ID() == "" and
	// the symmetric accessors return empty slices.
	KindUnknown ConceptKind = "unknown"
)

// Concept is the read-only handle returned by Match. Aliases are
// resolved server-side and intentionally not surfaced — compare on ID.
type Concept interface {
	// ID is the opaque entity identifier (today equals the server-side
	// MakeKey form). Empty string when IsKnown returns false.
	ID() string
	// Name is the display name from the catalog. Falls back to
	// InputText when IsKnown returns false.
	Name() string
	// Kind discriminates this handle.
	Kind() ConceptKind
	// IsKnown reports whether the input text matched a known entity.
	IsKnown() bool
	// InputText is the verbatim text passed to Match.
	InputText() string
	// Conditions associated with this concept. Defined on medication
	// handles; on condition or unknown handles this returns an empty
	// slice. Sort defaults to SortMostCommonFirst.
	Conditions(sort Sort) []ConditionConcept
	// Medications associated with this concept. Defined on condition
	// handles; on medication or unknown handles this returns an empty
	// slice.
	Medications(sort Sort) []MedicationConcept
	// Equals reports whether two concept handles refer to the same
	// catalog entity. Two unknown handles are equal iff their normalized
	// input text matches.
	Equals(other Concept) bool
}

// MedicationConcept is a Concept whose Kind() is statically
// KindMedication.
type MedicationConcept interface {
	Concept
	medicationMarker()
}

// ConditionConcept is a Concept whose Kind() is statically
// KindCondition.
type ConditionConcept interface {
	Concept
	conditionMarker()
}

// concept is the unexported implementation backing every kind. The
// marker methods on the public interfaces are satisfied by the embedded
// kind type below.
type concept struct {
	id        string
	name      string
	kind      ConceptKind
	isKnown   bool
	inputText string
	catalog   *catalog
}

func (c *concept) ID() string        { return c.id }
func (c *concept) Name() string      { return c.name }
func (c *concept) Kind() ConceptKind { return c.kind }
func (c *concept) IsKnown() bool     { return c.isKnown }
func (c *concept) InputText() string { return c.inputText }

func (c *concept) Equals(other Concept) bool {
	if other == nil {
		return false
	}
	if c.kind != other.Kind() {
		return false
	}
	if c.isKnown != other.IsKnown() {
		return false
	}
	if c.isKnown {
		return c.id == other.ID()
	}
	// Two unknown handles match when their normalized input text is
	// the same. This makes Equals a value comparison consistent with the
	// rest of the namespace (consumers don't carry opaque pointers).
	return makeKey(c.inputText) == makeKey(other.InputText())
}

func (c *concept) Conditions(s Sort) []ConditionConcept {
	if c.kind != KindMedication || c.catalog == nil {
		return []ConditionConcept{}
	}
	ids := c.catalog.conditionsForMedication(c.id)
	ordered := orderConditionIDsForMedication(c.catalog, ids, c.id, s)
	out := make([]ConditionConcept, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, buildConditionConcept(c.catalog, id, c.inputText))
	}
	return out
}

func (c *concept) Medications(s Sort) []MedicationConcept {
	if c.kind != KindCondition || c.catalog == nil {
		return []MedicationConcept{}
	}
	ids := c.catalog.medicationsForCondition(c.id)
	ordered := orderMedicationIDsForCondition(c.catalog, ids, c.id, s)
	out := make([]MedicationConcept, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, buildMedicationConcept(c.catalog, id, c.inputText))
	}
	return out
}

// medicationConcept / conditionConcept embed *concept and add the marker
// method that distinguishes the public interface. They are not exported
// — consumers receive them via the MedicationConcept / ConditionConcept
// interfaces.
type medicationConcept struct{ *concept }

func (medicationConcept) medicationMarker() {}

type conditionConcept struct{ *concept }

func (conditionConcept) conditionMarker() {}

func buildMedicationConcept(cat *catalog, id, inputText string) MedicationConcept {
	name, _ := cat.medicationName(id)
	if name == "" {
		name = inputText
	}
	return medicationConcept{&concept{
		id:        id,
		name:      name,
		kind:      KindMedication,
		isKnown:   true,
		inputText: inputText,
		catalog:   cat,
	}}
}

func buildConditionConcept(cat *catalog, id, inputText string) ConditionConcept {
	name, _ := cat.conditionName(id)
	if name == "" {
		name = inputText
	}
	return conditionConcept{&concept{
		id:        id,
		name:      name,
		kind:      KindCondition,
		isKnown:   true,
		inputText: inputText,
		catalog:   cat,
	}}
}

func buildUnknownConcept(inputText string) Concept {
	return &concept{
		id:        "",
		name:      inputText,
		kind:      KindUnknown,
		isKnown:   false,
		inputText: inputText,
	}
}
