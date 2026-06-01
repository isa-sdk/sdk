package reference

// MedicationsMatcher resolves free text against the medication catalog.
// Obtain via Index.Medications.
type MedicationsMatcher struct {
	cat *catalog
}

// Match resolves text against the medication catalog. On a hit, returns
// a MedicationConcept handle; on a miss, returns an unknown Concept
// with IsKnown()==false and InputText() preserved. Never returns an
// error.
func (m *MedicationsMatcher) Match(text string) Concept {
	key := makeKey(text)
	if key != "" {
		if _, ok := m.cat.medicationName(key); ok {
			return buildMedicationConcept(m.cat, key, text)
		}
	}
	return buildUnknownConcept(text)
}

// List returns every known medication in the catalog as a typed
// MedicationConcept slice, in catalog (display) order. The returned
// slice is freshly allocated; callers may mutate it.
func (m *MedicationsMatcher) List() []MedicationConcept {
	if m.cat == nil {
		return []MedicationConcept{}
	}
	out := make([]MedicationConcept, 0, len(m.cat.medicationOrder))
	for _, id := range m.cat.medicationOrder {
		name, _ := m.cat.medicationName(id)
		out = append(out, buildMedicationConcept(m.cat, id, name))
	}
	return out
}

// ConditionsMatcher resolves free text against the condition catalog.
// Obtain via Index.Conditions.
type ConditionsMatcher struct {
	cat *catalog
}

// Match resolves text against the condition catalog. On a hit, returns
// a ConditionConcept handle; on a miss, returns an unknown Concept.
// Never returns an error.
func (m *ConditionsMatcher) Match(text string) Concept {
	key := makeKey(text)
	if key != "" {
		if _, ok := m.cat.conditionName(key); ok {
			return buildConditionConcept(m.cat, key, text)
		}
	}
	return buildUnknownConcept(text)
}

// List returns every known condition in the catalog as a typed
// ConditionConcept slice, in catalog (display) order.
func (m *ConditionsMatcher) List() []ConditionConcept {
	if m.cat == nil {
		return []ConditionConcept{}
	}
	out := make([]ConditionConcept, 0, len(m.cat.conditionOrder))
	for _, id := range m.cat.conditionOrder {
		name, _ := m.cat.conditionName(id)
		out = append(out, buildConditionConcept(m.cat, id, name))
	}
	return out
}

// ConceptsMatcher resolves free text without specifying a kind. Tries
// conditions first (the typical "the user typed a symptom" case), then
// medications. Obtain via Index.Concepts.
type ConceptsMatcher struct {
	cat *catalog
}

// Match resolves text against the catalog without a kind constraint.
// Returns a ConditionConcept on a condition hit, a MedicationConcept on
// a medication hit, or an unknown Concept on a miss. Never returns an
// error.
func (m *ConceptsMatcher) Match(text string) Concept {
	key := makeKey(text)
	if key == "" {
		return buildUnknownConcept(text)
	}
	if _, ok := m.cat.conditionName(key); ok {
		return buildConditionConcept(m.cat, key, text)
	}
	if _, ok := m.cat.medicationName(key); ok {
		return buildMedicationConcept(m.cat, key, text)
	}
	return buildUnknownConcept(text)
}

// MatchMany resolves a batch of free-text terms in input order. Each
// returned Concept corresponds positionally to the input texts slice.
// Misses become unknown handles. Never returns an error.
func (m *ConceptsMatcher) MatchMany(texts []string) []Concept {
	out := make([]Concept, len(texts))
	for i, t := range texts {
		out[i] = m.Match(t)
	}
	return out
}
