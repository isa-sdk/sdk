package zyins

// BundledAPIVersions records which /vN each surface targets in this
// SDK release. The map is the SDK's declared intent: per-instance
// overrides flow through [LicenseOptions.APIVersion] /
// [BearerOptions.APIVersion] / [SessionOptions.APIVersion] on the
// parent sdk package and supersede the bundled value for that
// surface alone.
//
// Surface keys are stable strings shared across language SDKs
// (matching the TS and Python BUNDLED_API_VERSIONS sets) so a
// per-surface override carries the same meaning regardless of which
// SDK the caller uses. Surfaces NOT present here cannot be
// overridden — adding a new surface requires a deliberate SDK
// release.
var BundledAPIVersions = map[string]string{
	"prequalify": "v2",
	"quote":      "v2",
	"datasets":   "v2",
	"reference":  "v2",
	"sessions":   "v1",
	"branding":   "v1",
	"cases":      "v1",
}

// ResolveAPIVersion returns the version pinned for surface, applying
// per-instance overrides on top of [BundledAPIVersions]. A surface
// absent from both the override map and BundledAPIVersions returns an
// empty string — callers MUST treat the empty value as a configuration
// error rather than a default.
//
// The function takes no default: there is no global "/v1" fallback.
// Every surface that ships an override must be declared in
// BundledAPIVersions at release time.
func ResolveAPIVersion(overrides map[string]string, surface string) string {
	if v, ok := overrides[surface]; ok && v != "" {
		return v
	}
	if v, ok := BundledAPIVersions[surface]; ok {
		return v
	}
	return ""
}
