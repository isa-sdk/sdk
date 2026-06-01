package zyins

import (
	"os"
	"strconv"
	"strings"
)

// legacyWireEnabled reports whether outbound requests should use the
// engine's legacy flat-body shape (date_of_birth, gender, quote_options)
// instead of the typed applicant envelope. Conformance CI sets
// ZYINS_LEGACY_WIRE=1 while the live API and SDK wire formats converge.
func legacyWireEnabled() bool {
	return strings.TrimSpace(os.Getenv("ZYINS_LEGACY_WIRE")) == "1"
}

// legacyEngineBodyFromApplicant builds the flat JSON body the live
// engine still accepts on /v1/prequalify and /v1/quote.
func legacyEngineBodyFromApplicant(a Applicant) map[string]any {
	sex := "male"
	if a.Sex == SexFemale {
		sex = "female"
	}
	nicotine := a.resolveNicotineUsageInput().LastUsed == NicotineWithin12Months
	body := map[string]any{
		"date_of_birth": a.DOB,
		"gender":        sex,
		"state":         a.State,
		"height":        a.Height.TotalInches,
		"weight":        a.Weight.Pounds,
		"nicotine_usage": map[string]any{
			"is_nicotine_user": nicotine,
		},
	}
	if len(a.Conditions) > 0 {
		conds := make([]map[string]any, 0, len(a.Conditions))
		for _, c := range a.Conditions {
			conds = append(conds, map[string]any{
				"name":           c.Name,
				"was_diagnosed":  c.WasDiagnosed,
				"last_treatment": c.LastTreatment,
			})
		}
		body["conditions"] = conds
	}
	if len(a.Medications) > 0 {
		meds := make([]map[string]any, 0, len(a.Medications))
		for _, m := range a.Medications {
			meds = append(meds, map[string]any{
				"name":       m.Name,
				"use":        m.Use,
				"first_fill": m.FirstFill,
				"last_fill":  m.LastFill,
			})
		}
		body["medications"] = meds
	}
	return body
}

func legacyPrequalifyBodyFromApplicant(a Applicant, faceValue int) map[string]any {
	body := legacyEngineBodyFromApplicant(a)
	body["quote_options"] = map[string]any{
		"quote_type": "face_amounts",
		"amounts":    []string{strconv.Itoa(faceValue)},
	}
	return body
}

func legacyQuoteBodyFromApplicant(a Applicant, faceValue int) map[string]any {
	body := legacyEngineBodyFromApplicant(a)
	body["quote_options"] = map[string]any{
		"face_amounts":  []int{faceValue},
		"pricing_modes": []string{"MONTHLY-EFT"},
	}
	return body
}

// defaultLegacyFaceAmount reads the requested face value from coverage.
func defaultLegacyFaceAmount(c Coverage) int {
	if c.Type == CoverageFaceValue && c.Amount > 0 {
		return c.Amount
	}
	return 25_000
}
