package zyins

import (
	"errors"
	"fmt"

	"github.com/isa-sdk/sdk/catalog"
)

// State is a re-export of catalog.State so callers can write
// `zyins.State("NC")` or use the typed catalog constants
// (`catalog.StateNorthCarolina`) without importing the catalog package
// directly. The type alias (not a defined type) preserves identity with
// catalog.State, so values are freely assignable in either direction.
//
// Idiotproof usage:
//
//	applicant := zyins.Applicant{State: catalog.StateNorthCarolina, /* … */}
//
// Untyped string constants (`State: "NC"`) keep compiling — Go assigns
// untyped strings to named string types implicitly. The typed form
// catches typos like `"North Carolina"` at the catalog lookup edge.
type State = catalog.State

// Sex is the applicant's biological sex. The canonical wire values are
// "male" and "female" — emit string(sex) directly at the wire layer.
type Sex string

const (
	SexMale   Sex = "male"
	SexFemale Sex = "female"
)

// SexWireCode returns the canonical wire string for s.
//
// Deprecated: Use string(sex) directly. Sex values are already the
// canonical wire tokens ("male"/"female") as of v0.5.1. Will be removed
// in v0.7.0.
func SexWireCode(s Sex) (string, error) {
	switch s {
	case SexMale, SexFemale:
		return string(s), nil
	default:
		return "", fmt.Errorf("zyins: SexWireCode: unknown Sex value %q (must be %q or %q)", string(s), string(SexMale), string(SexFemale))
	}
}

// NicotineDuration is the seven-bucket duration encoding for nicotine
// use history. It is the preferred form for the 0.5.1 wire.
type NicotineDuration string

const (
	NicotineNever          NicotineDuration = "never"
	NicotineWithin12Months NicotineDuration = "within_12_months"
	Nicotine12To24Months   NicotineDuration = "12_to_24_months"
	Nicotine24To36Months   NicotineDuration = "24_to_36_months"
	Nicotine36To48Months   NicotineDuration = "36_to_48_months"
	Nicotine48To60Months   NicotineDuration = "48_to_60_months"
	NicotineOver60Months   NicotineDuration = "over_60_months"
)

// NicotineProductUsage records how frequently a specific nicotine
// product type is used. Used inside NicotineUsageInput.
type NicotineProductUsage struct {
	// Type is the product name (e.g., "cigarettes", "cigars").
	Type string `json:"type"`
	// Frequency is a descriptive rate (e.g., "daily", "occasional").
	Frequency string `json:"frequency"`
}

// NicotineUsageInput is the structured, duration-aware nicotine input
// for the 0.5.1 wire. Prefer this over the deprecated NicotineUsage
// enum for new code.
type NicotineUsageInput struct {
	// LastUsed is the duration bucket for the most recent nicotine use.
	LastUsed NicotineDuration
	// ProductUsage is an optional breakdown by product type.
	ProductUsage []NicotineProductUsage
}

// NicotineUsage captures current/former/none nicotine status.
//
// Deprecated: Use NicotineUsageInput with NicotineDuration constants.
// Will be removed in v0.7.0.
type NicotineUsage string

const (
	NicotineNone    NicotineUsage = "none"
	NicotineCurrent NicotineUsage = "current"
	NicotineFormer  NicotineUsage = "former"
)

// inchesPerFoot is the only unit conversion the SDK performs locally.
const inchesPerFoot = 12

// Height is the applicant's height in total inches. Use NewHeight to
// construct from feet+inches; the value type is the engine's normalized
// form.
type Height struct {
	// TotalInches is the height as a single integer count. JSON-encoded
	// as `height_inches` per the wire format.
	TotalInches int `json:"height_inches"`
}

// NewHeight constructs a Height from feet+inches. Both arguments must
// be non-negative.
func NewHeight(feet, inches int) (Height, error) {
	if feet < 0 || inches < 0 {
		return Height{}, errors.New("zyins: NewHeight requires non-negative feet and inches")
	}
	return Height{TotalInches: feet*inchesPerFoot + inches}, nil
}

// NewHeightInches constructs a Height from a raw inch total. Provided
// for parity tests; production call sites prefer NewHeight.
func NewHeightInches(total int) (Height, error) {
	if total < 0 {
		return Height{}, errors.New("zyins: NewHeightInches requires a non-negative total")
	}
	return Height{TotalInches: total}, nil
}

// Weight is the applicant's weight in pounds — the only unit the
// prequalify wire accepts. NewWeight exists so the call site reads
// `NewWeight(195)` rather than passing a bare integer without unit
// context.
type Weight struct {
	// Pounds is the weight as a positive integer. JSON-encoded as
	// `weight_pounds` per the wire format.
	Pounds int `json:"weight_pounds"`
}

// NewWeight constructs a Weight from a positive pound count.
func NewWeight(pounds int) (Weight, error) {
	if pounds <= 0 {
		return Weight{}, errors.New("zyins: NewWeight requires a positive pound count")
	}
	return Weight{Pounds: pounds}, nil
}

// Medication is a single drug on the applicant profile. All fields are
// strings as the engine accepts them — relative dates ("3 MONTHS AGO")
// rather than ISO dates for first/last fill.
type Medication struct {
	// Name as the applicant reports it (e.g., "LOSARTAN").
	Name string `json:"name"`
	// Use is the reason for use (e.g., "HIGH BLOOD PRESSURE").
	Use string `json:"use"`
	// FirstFill is a relative date string (e.g., "11 MONTHS AGO").
	FirstFill string `json:"first_fill"`
	// LastFill is a relative date string for the most recent fill.
	LastFill string `json:"last_fill"`
}

// Condition is a single medical condition on the applicant profile.
type Condition struct {
	// Name as the applicant reports it (e.g., "COPD", "HBP").
	Name string `json:"name"`
	// WasDiagnosed is a relative date string (e.g., "3 DAYS AGO").
	WasDiagnosed string `json:"was_diagnosed"`
	// LastTreatment is a relative date string for the most recent treatment.
	LastTreatment string `json:"last_treatment"`
}

// Applicant captures the underwriting profile the prequalify engine
// operates on. All fields except Zip, Medications, and Conditions are
// required; the engine refuses requests that omit them.
type Applicant struct {
	// DOB is the date of birth as an ISO 8601 date string ("1962-04-18").
	DOB string `json:"dob"`
	// Sex is the biological sex; serialized via SexWireCode at the
	// prequalify layer.
	Sex Sex `json:"-"`
	// Height in total inches.
	Height Height `json:"-"`
	// Weight in pounds.
	Weight Weight `json:"-"`
	// State is the ISO 3166-2:US two-letter postal code. Prefer the
	// typed catalog constants (e.g., catalog.StateNorthCarolina) over
	// raw string literals; both forms accept at the field, but the
	// typed form rejects typos like "North Carolina" at the call site.
	State State `json:"state"`
	// Zip is the postal code; required by some product families.
	Zip string `json:"zip,omitempty"`
	// NicotineUse is the underlying fact; mapped to the wire shape by the
	// prequalify builder. Set via NicotineUsageInput for 0.5.1+.
	//
	// Deprecated: NicotineUsageLegacy accepts the old NicotineUsage enum.
	// Will be removed in v0.7.0.
	NicotineUse NicotineUsageInput `json:"-"`
	// NicotineUsageLegacy holds the deprecated three-value enum when the
	// caller has not yet migrated to NicotineUsageInput.
	//
	// Deprecated: Use NicotineUse (NicotineUsageInput) instead. Will be
	// removed in v0.7.0.
	NicotineUsageLegacy NicotineUsage `json:"-"`
	// Medications is the applicant's drug list; may be empty.
	Medications []Medication `json:"medications,omitempty"`
	// Conditions is the applicant's medical history; may be empty.
	Conditions []Condition `json:"conditions,omitempty"`
}

// validate returns nil when the applicant carries every required field
// the engine demands. Sub-cases call this before serialization so the
// caller gets a typed error instead of a 400 round-trip.
func (a *Applicant) validate() error {
	if a == nil {
		return errors.New("zyins: applicant is nil")
	}
	if a.DOB == "" {
		return errors.New("zyins: applicant.DOB is required")
	}
	if a.Sex == "" {
		return errors.New("zyins: applicant.Sex is required")
	}
	if a.Sex != SexMale && a.Sex != SexFemale {
		return fmt.Errorf("zyins: applicant.Sex must be %q or %q, got %q", string(SexMale), string(SexFemale), string(a.Sex))
	}
	if a.Height.TotalInches <= 0 {
		return errors.New("zyins: applicant.Height is required")
	}
	if a.Weight.Pounds <= 0 {
		return errors.New("zyins: applicant.Weight is required")
	}
	if a.State == "" {
		return errors.New("zyins: applicant.State is required")
	}
	if a.NicotineUse.LastUsed == "" && a.NicotineUsageLegacy == "" {
		return errors.New("zyins: applicant.NicotineUse or NicotineUsageLegacy is required")
	}
	if a.NicotineUse.LastUsed != "" {
		switch a.NicotineUse.LastUsed {
		case NicotineNever,
			NicotineWithin12Months,
			Nicotine12To24Months,
			Nicotine24To36Months,
			Nicotine36To48Months,
			Nicotine48To60Months,
			NicotineOver60Months:
		default:
			return fmt.Errorf("zyins: applicant.NicotineUse.LastUsed %q is not a valid NicotineDuration constant", a.NicotineUse.LastUsed)
		}
	} else {
		switch a.NicotineUsageLegacy {
		case NicotineNone, NicotineCurrent, NicotineFormer:
		default:
			return fmt.Errorf("zyins: applicant.NicotineUsageLegacy must be %q, %q, or %q, got %q", NicotineNone, NicotineCurrent, NicotineFormer, a.NicotineUsageLegacy)
		}
	}
	return nil
}

// resolveNicotineUsageInput returns the NicotineUsageInput to serialize,
// mapping NicotineUsageLegacy to the nearest duration bucket when the
// caller used the deprecated field.
func (a *Applicant) resolveNicotineUsageInput() NicotineUsageInput {
	if a.NicotineUse.LastUsed != "" {
		return a.NicotineUse
	}
	// Legacy enum → nearest 0.5.1 bucket.
	var d NicotineDuration
	switch a.NicotineUsageLegacy {
	case NicotineNone:
		d = NicotineNever
	case NicotineCurrent:
		d = NicotineWithin12Months
	case NicotineFormer:
		d = Nicotine12To24Months
	default:
		return NicotineUsageInput{}
	}
	return NicotineUsageInput{LastUsed: d}
}
