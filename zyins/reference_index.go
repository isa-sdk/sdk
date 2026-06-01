package zyins

import (
	"github.com/isa-sdk/sdk/zyins/reference"
)

// NewReferenceIndex builds a typed reference.Index from a v3 datasets
// envelope. The returned Index owns immutable copies of the inputs and
// exposes `idx.Medications.Match(text)`, `idx.Conditions.Match(text)`,
// and `idx.Concepts.Match(text)` — the locked Concept-handle surface.
//
// Typical use:
//
//	res, err := client.DatasetsV3.Get(ctx, zyins.DatasetsV3Options{})
//	if err != nil { return err }
//	idx := client.NewReferenceIndex(res.Bundle)
//	hbp := idx.Conditions.Match("hbp")
//	meds := hbp.Medications(reference.SortMostCommonFirst)
//
// nil-safe: a nil bundle yields an empty Index whose every Match call
// returns an unknown handle. Rebuild on dataset version change; the
// Index is bound to one immutable snapshot.
func (c *Client) NewReferenceIndex(bundle *DatasetBundleV3) *reference.Index {
	return reference.NewIndex(referenceDatasetsFrom(bundle))
}

func referenceDatasetsFrom(bundle *DatasetBundleV3) *reference.DatasetsResponse {
	if bundle == nil {
		return nil
	}
	meds := make([]reference.Entity, len(bundle.Medications))
	for i, e := range bundle.Medications {
		meds[i] = reference.Entity{ID: e.ID, Name: e.Name}
	}
	conds := make([]reference.Entity, len(bundle.Conditions))
	for i, e := range bundle.Conditions {
		conds[i] = reference.Entity{ID: e.ID, Name: e.Name}
	}
	return &reference.DatasetsResponse{
		Version:             bundle.Version,
		Medications:         meds,
		Conditions:          conds,
		ConditionRelations:  relationEdgesToReference(bundle.ConditionRelations),
		MedicationRelations: relationEdgesToReference(bundle.MedicationRelations),
	}
}

func relationEdgesToReference(edges []RelationEdge) []reference.Relation {
	if len(edges) == 0 {
		return nil
	}
	out := make([]reference.Relation, len(edges))
	for i, e := range edges {
		out[i] = reference.Relation{
			FromID:            e.FromID,
			ToID:              e.ToID,
			ToName:            e.ToName,
			PrescriptionCount: e.PrescriptionCount,
		}
	}
	return out
}
