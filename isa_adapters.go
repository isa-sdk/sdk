// Top-level SDK reference adapter surface.
//
// Mirrors the locked spec at /tmp/v3-datasets-adapter-cutover-spec.md
// §2 "Surface": the generic kernel (`isa.Autocorrector()` factory)
// returns a [reference.Autocorrector] built from any caller-supplied
// typo map, with no dependency on the zyins datasets. The pre-bound
// surface is reached via `isa.Zyins.Autocorrector(ctx)`.

package sdk

import (
	"github.com/isa-sdk/sdk/zyins/reference"
)

// AutocorrectorFactory mirrors the TS `isa.autocorrector` namespace:
// a tiny builder whose Create method returns a fresh
// [reference.Autocorrector] keyed on a caller-supplied typo map.
//
// Construct via [(*Isa).Autocorrector].
type AutocorrectorFactory struct{}

// Create returns a [reference.DefaultAutocorrector] bound to typoMap.
// Subsequent calls return independent instances; mutate typoMap before
// or after at the caller's discretion (the constructor copies it).
func (AutocorrectorFactory) Create(typoMap map[string]string, opts ...reference.AutocorrectorOption) reference.Autocorrector {
	return reference.NewDefaultAutocorrector(typoMap, opts...)
}

// Autocorrector returns the kernel-level autocorrector factory.
// Equivalent to TS `isa.autocorrector` (per /tmp/v3-datasets-adapter-
// cutover-spec.md §2). The domain-bound autocorrector that consumes
// the v3 spelling_corrections dataset lives on [(*Isa).Zyins.Autocorrector].
func (i *Isa) Autocorrector() AutocorrectorFactory {
	return AutocorrectorFactory{}
}

// MatchAlgorithmFactory mirrors the TS `isa.matchAlgorithm` namespace:
// returns the default key-equality matcher (or a clone with options).
type MatchAlgorithmFactory struct{}

// Create returns a [reference.DefaultMatchAlgorithm].
func (MatchAlgorithmFactory) Create(opts ...reference.MatchAlgorithmOption) reference.MatchAlgorithm {
	return reference.NewDefaultMatchAlgorithm(opts...)
}

// MatchAlgorithm returns the kernel-level match algorithm factory.
func (i *Isa) MatchAlgorithm() MatchAlgorithmFactory {
	return MatchAlgorithmFactory{}
}

// AutocompleteAlgorithmFactory mirrors the TS `isa.autocompleteAlgorithm`
// namespace.
type AutocompleteAlgorithmFactory struct{}

// Create returns a [reference.DefaultAutocompleteAlgorithm].
func (AutocompleteAlgorithmFactory) Create(opts ...reference.AutocompleteAlgorithmOption) reference.AutocompleteAlgorithm {
	return reference.NewDefaultAutocompleteAlgorithm(opts...)
}

// AutocompleteAlgorithm returns the kernel-level autocomplete factory.
func (i *Isa) AutocompleteAlgorithm() AutocompleteAlgorithmFactory {
	return AutocompleteAlgorithmFactory{}
}

// WithAutocorrector installs a caller-supplied
// [reference.Autocorrector] on the underlying zyins client. Pass nil
// to clear any prior override and restore the dataset-derived default
// (built lazily from the v3 spelling_corrections rows).
//
// Returns the receiver so calls chain.
func (i *Isa) WithAutocorrector(a reference.Autocorrector) *Isa {
	if i == nil || i.Zyins == nil {
		return i
	}
	i.Zyins.SetAutocorrector(a)
	return i
}

// WithMatchAlgorithm installs a caller-supplied
// [reference.MatchAlgorithm]. Pass nil to restore the key-equality
// default.
func (i *Isa) WithMatchAlgorithm(m reference.MatchAlgorithm) *Isa {
	if i == nil || i.Zyins == nil {
		return i
	}
	i.Zyins.SetMatchAlgorithm(m)
	return i
}

// WithAutocompleteAlgorithm installs a caller-supplied
// [reference.AutocompleteAlgorithm]. Pass nil to restore the bucketed
// default.
func (i *Isa) WithAutocompleteAlgorithm(a reference.AutocompleteAlgorithm) *Isa {
	if i == nil || i.Zyins == nil {
		return i
	}
	i.Zyins.SetAutocompleteAlgorithm(a)
	return i
}
