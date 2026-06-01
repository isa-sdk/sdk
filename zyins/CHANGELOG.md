# Changelog — github.com/Software-Automation-Holdings-LLC/sdk/zyins

All notable changes to the ZyINS Go SDK. The package has not yet been
tagged for release; until v0.1.0 ships, every release is implicitly a
pre-release and the API may change in incompatible ways.

## Unreleased

### Changed

- `LicenseService.Activate`, `LicenseService.Check`, and
  `LicenseService.Deactivate` now target the v2 bootstrap surface at
  `/v2/licenses/{activate,check,deactivate}`. These three operations
  sit outside `AuthMiddleware` on the server (activate is what mints
  the licenseKey, so the client cannot sign with a credential it does
  not yet have); the SDK strips the `Authorization` header for these
  calls and routes them through an unwrapped HTTP doer. Wire body keys
  are now camelCase (`deviceId`, `licenseKey`) to match the v2
  contract. The activate response surfaces `licenseKey` at the top
  level of `data` (legacy snake_case + nested `auth.license_key`
  spellings still parse). `result.LicenseKey` remains the canonical
  credential field, and `result.Auth.LicenseKey` mirrors it for TS SDK
  parity. `LicenseDeactivateResult.RemainingActivations` is a pointer
  so legacy responses that omit the field stay distinguishable from v2
  responses that explicitly report zero.
  Mirrors TS SDK PR #302 (`@isa-platform/sdk-ts` v0.5.5).
- `userAgentHeader` bumped to `isa-sdk-zyins-go/0.5.5`.

### Breaking

- `LicenseActivateResult` adds `Auth` and
  `LicenseDeactivateResult.RemainingActivations` is now `*int` instead
  of `int`. This is safe for the current pre-release package, which has
  not been published to a registry yet.
- `SexWireCode(s Sex)` now returns `(string, error)` instead of `string`.
  Unknown values previously silently mapped to `"F"`, which masked
  caller bugs. Callers must now propagate the error. This ripples
  through `buildPrequalifyBody` (now returns `(prequalifyWireBody,
  error)`) and `QuoteService.Run`, but both are internal to the SDK —
  no external Go callers exist yet because the package has not been
  published to a registry (the publish workflow gates on tag push and
  no tag exists).

### Fixed

- `DatasetsService` no longer silently produces a zero-value page when
  the server returns `{"data": null}`; the SDK now returns an explicit
  error.
- `validateTokenShape` and `StaticToken.Token` now reject tokens with
  leading or trailing whitespace instead of silently trimming them.
  Surrounding whitespace is almost always an env-loading bug and
  trimming masked it until the first authenticated request failed
  with a confusing 401.
- `NewProductSelection` and `NewProductSelectionFromProducts` reject
  product tokens with surrounding whitespace for the same reason —
  the wire contract treats the token as opaque, so silently trimming
  would diverge from what the caller stored.
- `Applicant.validate` now rejects unknown `Sex` values (anything other
  than `SexMale` / `SexFemale`) instead of accepting any non-empty
  string.
- Package-level documentation example in `doc.go` now compiles:
  `NewFaceValueCoverage` and `NewProductSelection` are shown with
  their `(value, error)` return shape.
- `WithMaxRetryAttempts` docstring corrected to match the
  implementation: zero and negative counts are programming errors and
  rejected, not a fallback to the SDK default. Callers who want the
  default should omit the option entirely.
- Request error messages now include the logical operation name (e.g.,
  `[op=prequalify]`) so failures inside shared helpers like
  `listDataset` still identify which API call triggered them.

### Internal

- Renamed test cases `WithMaxRetryAttempts_neg` to
  `WithMaxRetryAttempts_zero` / `WithMaxRetryAttempts_negative` to
  match the inputs they actually exercise.
- Clarified comment on `deriveIdempotencyKey` — the function is
  unexported and not part of the public surface.
