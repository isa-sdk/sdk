package sdk_test

import (
	"context"
	"testing"

	zyins "github.com/isa-sdk/sdk/zyins"
)

// TestDeversionedQuickstartExample_Compiles is a compile-only guard for the
// de-versioned symbols the public quickstart guides reference
// (api/guides/quickstart/go.md). It must mention the canonical, unversioned
// names — Prequalify, Quote, PrequalifyRequest, QuoteRequest — so the guide
// examples cannot drift away from a compiling SDK surface. It does not hit
// the wire; the function body is never invoked.
func TestDeversionedQuickstartExample_Compiles(t *testing.T) {
	t.Parallel()
	_ = func(isaZyins *zyins.Client) (*zyins.PrequalifyV3Result, *zyins.QuoteV3Result, error) {
		ctx := context.Background()
		applicant := zyins.Applicant{
			DOB:         "1962-04-18",
			Sex:         zyins.SexMale,
			State:       "NC",
			NicotineUse: zyins.NicotineUsageInput{LastUsed: zyins.NicotineNever},
		}
		pq, err := isaZyins.Prequalify.Run(ctx, &zyins.PrequalifyRequest{Applicant: applicant})
		if err != nil {
			return nil, nil, err
		}
		q, err := isaZyins.Quote.Run(ctx, &zyins.QuoteRequest{Applicant: applicant})
		if err != nil {
			return pq, nil, err
		}
		return pq, q, nil
	}
}
