package reference_test

import (
	"context"
	"fmt"

	"github.com/isa-sdk/sdk/zyins/reference"
)

// ExampleNewDefaultAutocorrector shows the construction shape every
// caller uses: build the typo map once (it is read-only after
// construction), inject options, call Correct.
func ExampleNewDefaultAutocorrector() {
	typoMap := map[string]string{
		"HYPRTENSION":  "HYPERTENSION",
		"HOSPITILIZED": "HOSPITALIZED",
	}
	ac := reference.NewDefaultAutocorrector(typoMap,
		reference.WithAutocorrectorVersionTag("2026.05.29"),
	)
	out := ac.Correct(context.Background(), "Patient HOSPITILIZED last year",
		reference.CorrectOptions{Mode: reference.AutocorrectModeSubmit})
	fmt.Println(out)
	fmt.Println(ac.VersionTag())
	// Output:
	// PATIENT HOSPITALIZED LAST YEAR
	// 2026.05.29
}

// ExampleDefaultAutocorrector_Correct shows the keyup vs submit guards.
// Keyup leaves mid-typing input alone; submit rewrites it. The same map
// produces different output depending on the typing-state mode.
func ExampleDefaultAutocorrector_Correct() {
	ac := reference.NewDefaultAutocorrector(map[string]string{
		"ASTHM": "ASTHMA",
	})
	keyup := ac.Correct(context.Background(), "asthm",
		reference.CorrectOptions{Mode: reference.AutocorrectModeKeyup})
	submit := ac.Correct(context.Background(), "asthm",
		reference.CorrectOptions{Mode: reference.AutocorrectModeSubmit})
	fmt.Printf("keyup=%q submit=%q\n", keyup, submit)
	// Output:
	// keyup="ASTHM" submit="ASTHMA"
}

// ExampleDefaultAutocorrector_Correct_submitGuardsDuplication shows the
// submit-mode guard: if the input already contains the correction
// verbatim, leave it alone.
func ExampleDefaultAutocorrector_Correct_submitGuardsDuplication() {
	ac := reference.NewDefaultAutocorrector(map[string]string{
		"HIGH CHOLESTEROL": "HIGH CHOLESTEROL",
	})
	out := ac.Correct(context.Background(), "HIGH CHOLESTEROL",
		reference.CorrectOptions{Mode: reference.AutocorrectModeSubmit})
	fmt.Println(out)
	// Output:
	// HIGH CHOLESTEROL
}

// ExampleDefaultAutocorrector_Clone shows the wholesale-replacement
// pattern: take a pre-wired default, clone with overrides (e.g. a
// per-tenant version tag or onApplied callback).
func ExampleDefaultAutocorrector_Clone() {
	base := reference.NewDefaultAutocorrector(map[string]string{
		"HBP": "HIGH BLOOD PRESSURE",
	}, reference.WithAutocorrectorVersionTag("base"))

	var captured []reference.AutocorrectEvent
	tenant := base.Clone(
		reference.WithAutocorrectorVersionTag("tenant-acme"),
		reference.WithAutocorrectorOnApplied(func(e reference.AutocorrectEvent) {
			captured = append(captured, e)
		}),
	)

	_ = tenant.Correct(context.Background(), "hbp",
		reference.CorrectOptions{Mode: reference.AutocorrectModeSubmit})
	fmt.Println(base.VersionTag(), tenant.VersionTag(), len(captured))
	// Output:
	// base tenant-acme 1
}
