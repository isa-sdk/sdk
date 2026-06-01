// Package zyins is the Go SDK for the ZyINS prequalify and quote APIs.
//
// Construct a Client with a bearer token and use the typed sub-services:
//
//	client, err := zyins.NewClient(zyins.WithToken("isa_live_..."))
//	if err != nil {
//		return err
//	}
//	coverage, err := zyins.NewFaceValueCoverage(100_000)
//	if err != nil {
//		return err
//	}
//	products, err := zyins.NewProductSelection("colonial-penn.final-expense")
//	if err != nil {
//		return err
//	}
//	result, err := client.Prequalify.Run(ctx, &zyins.PrequalifyInput{
//		Applicant: zyins.Applicant{ /* ... */ },
//		Coverage:  coverage,
//		Products:  products,
//	})
//
// The Client is safe for concurrent use; construct one per token and
// reuse it across goroutines. All blocking methods accept context.Context
// as the first argument.
//
// Authentication is bearer-token only at the SDK layer: the constructor
// receives the token and the transport injects `Authorization: Bearer
// <token>` on every request. Idempotency keys are generated automatically
// for mutating verbs unless WithIdempotencyKey overrides them per call.
//
// Errors funnel through the typed *Error hierarchy. Match on the typed
// subclasses (LicenseError, ValidationError, RateLimitError, AuthError)
// with errors.As, or switch on the stable Code field of the base *Error.
//
// The package composes the BearerTransport / RetryTransport / ExtractData
// primitives from sdk/core; the public surface here is wire-format-aware
// types and the operation builders.
package zyins
