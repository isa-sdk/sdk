package zyins

import "testing"

func TestSexWireCode_MapsMaleAndFemale(t *testing.T) {
	got, err := SexWireCode(SexMale)
	if err != nil || got != "male" {
		t.Errorf("SexWireCode(Male) = (%q, %v), want (male, nil)", got, err)
	}
	got, err = SexWireCode(SexFemale)
	if err != nil || got != "female" {
		t.Errorf("SexWireCode(Female) = (%q, %v), want (female, nil)", got, err)
	}
}

func TestSexWireCode_UnknownValueReturnsError(t *testing.T) {
	cases := []Sex{"", "unknown", "MALE", "f", "x", "M", "F"}
	for _, s := range cases {
		got, err := SexWireCode(s)
		if err == nil {
			t.Errorf("SexWireCode(%q) = (%q, nil); want error", string(s), got)
		}
		if got != "" {
			t.Errorf("SexWireCode(%q) returned non-empty %q on error", string(s), got)
		}
	}
}

func TestApplicant_Validate_RejectsUnknownSex(t *testing.T) {
	h, _ := NewHeight(5, 10)
	w, _ := NewWeight(195)
	a := Applicant{
		DOB:         "1962-04-18",
		Sex:         Sex("MALE"), // wrong casing — not a recognized enum value
		Height:      h,
		Weight:      w,
		State:       "NC",
		NicotineUse: NicotineUsageInput{LastUsed: NicotineNever},
	}
	if err := a.validate(); err == nil {
		t.Errorf("validate() accepted unknown Sex value")
	}
}

func TestNewHeight_FeetInches(t *testing.T) {
	h, err := NewHeight(5, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.TotalInches != 70 {
		t.Errorf("TotalInches = %d, want 70", h.TotalInches)
	}
}

func TestNewHeight_NegativeReturnsError(t *testing.T) {
	if _, err := NewHeight(-1, 0); err == nil {
		t.Errorf("expected error for negative feet")
	}
	if _, err := NewHeight(5, -1); err == nil {
		t.Errorf("expected error for negative inches")
	}
}

func TestNewWeight_PositiveOnly(t *testing.T) {
	w, err := NewWeight(195)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Pounds != 195 {
		t.Errorf("Pounds = %d, want 195", w.Pounds)
	}
	if _, err := NewWeight(0); err == nil {
		t.Errorf("expected error for zero")
	}
	if _, err := NewWeight(-1); err == nil {
		t.Errorf("expected error for negative")
	}
}

func TestApplicant_Validate_RequiresFields(t *testing.T) {
	full := func() Applicant {
		h, _ := NewHeight(5, 10)
		w, _ := NewWeight(195)
		return Applicant{
			DOB:         "1962-04-18",
			Sex:         SexMale,
			Height:      h,
			Weight:      w,
			State:       "NC",
			NicotineUse: NicotineUsageInput{LastUsed: NicotineNever},
		}
	}
	a := full()
	if err := a.validate(); err != nil {
		t.Fatalf("complete applicant should validate; got %v", err)
	}

	cases := map[string]func(*Applicant){
		"missing dob":      func(a *Applicant) { a.DOB = "" },
		"missing sex":      func(a *Applicant) { a.Sex = "" },
		"missing height":   func(a *Applicant) { a.Height = Height{} },
		"missing weight":   func(a *Applicant) { a.Weight = Weight{} },
		"missing state":    func(a *Applicant) { a.State = "" },
		"missing nicotine": func(a *Applicant) { a.NicotineUse = NicotineUsageInput{}; a.NicotineUsageLegacy = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a := full()
			mutate(&a)
			if err := a.validate(); err == nil {
				t.Errorf("expected validation error after %s", name)
			}
		})
	}
}

func TestNicotineDuration_Values(t *testing.T) {
	cases := []struct {
		constant NicotineDuration
		wire     string
	}{
		{NicotineNever, "never"},
		{NicotineWithin12Months, "within_12_months"},
		{Nicotine12To24Months, "12_to_24_months"},
		{Nicotine24To36Months, "24_to_36_months"},
		{Nicotine36To48Months, "36_to_48_months"},
		{Nicotine48To60Months, "48_to_60_months"},
		{NicotineOver60Months, "over_60_months"},
	}
	for _, c := range cases {
		if string(c.constant) != c.wire {
			t.Errorf("NicotineDuration %q has wire value %q, want %q", c.constant, string(c.constant), c.wire)
		}
	}
}

func TestApplicant_LegacyNicotine_MapsToInput(t *testing.T) {
	h, _ := NewHeight(5, 10)
	w, _ := NewWeight(195)
	a := Applicant{
		DOB:                 "1962-04-18",
		Sex:                 SexMale,
		Height:              h,
		Weight:              w,
		State:               "NC",
		NicotineUsageLegacy: NicotineNone,
	}
	if err := a.validate(); err != nil {
		t.Fatalf("legacy nicotine should pass validate: %v", err)
	}
	resolved := a.resolveNicotineUsageInput()
	if resolved.LastUsed != NicotineNever {
		t.Errorf("NicotineNone should resolve to NicotineNever, got %q", resolved.LastUsed)
	}

	a.NicotineUsageLegacy = NicotineCurrent
	if a.resolveNicotineUsageInput().LastUsed != NicotineWithin12Months {
		t.Errorf("NicotineCurrent should resolve to NicotineWithin12Months")
	}

	a.NicotineUsageLegacy = NicotineFormer
	if a.resolveNicotineUsageInput().LastUsed != Nicotine12To24Months {
		t.Errorf("NicotineFormer should resolve to Nicotine12To24Months")
	}
}

func TestApplicant_Validate_RejectsUnknownLegacyNicotine(t *testing.T) {
	h, _ := NewHeight(5, 10)
	w, _ := NewWeight(195)
	a := Applicant{
		DOB:                 "1962-04-18",
		Sex:                 SexMale,
		Height:              h,
		Weight:              w,
		State:               "NC",
		NicotineUsageLegacy: NicotineUsage("sometimes"),
	}

	if err := a.validate(); err == nil {
		t.Fatalf("validate accepted unknown legacy nicotine value")
	}
}
