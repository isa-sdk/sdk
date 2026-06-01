package zyins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// prequalifyPath is the canonical path for the prequalify operation.
const prequalifyPath = "/v1/prequalify"

// PrequalifyService is the typed sub-service exposing the prequalify
// operation. Constructed by NewClient; do not construct directly.
type PrequalifyService struct {
	client *Client
}

// PrequalifyInput is the typed request shape for Prequalify.Run.
type PrequalifyInput struct {
	// Applicant captures the underwriting profile. All inner required
	// fields must be populated; Run returns *ValidationError otherwise.
	Applicant Applicant
	// Coverage selects either a face-value or monthly-budget request.
	Coverage Coverage
	// Products is the list of carrier/type combinations to evaluate.
	Products ProductSelection
}

// PrequalifyResult is the typed response shape for Prequalify.Run.
type PrequalifyResult struct {
	// Plans is the engine's qualified list, ordered by ranking.
	Plans []PrequalifyPlan `json:"plans"`
	// RequestID is the server's correlation identifier; surface in
	// logs and support tickets.
	RequestID string `json:"request_id"`
}

// PrequalifyPlan is one plan the engine accepted for the applicant.
type PrequalifyPlan struct {
	// Brand is the carrier identifier (e.g., "colonial-penn").
	Brand string `json:"brand"`
	// Tier is the plan tier within the carrier.
	Tier string `json:"tier"`
	// MonthlyPremiumCents is the bucketed monthly premium in USD cents.
	// Money is integer cents (not float) to avoid binary-float rounding
	// error; format to a UI string by dividing by 100.
	MonthlyPremiumCents int64 `json:"monthly_premium_cents"`
	// FaceValueCents is the death benefit in USD cents.
	FaceValueCents int64 `json:"face_value_cents"`
	// ProductToken is the wire token; useful for routing into eApp.
	ProductToken string `json:"product_token"`
}

// RunOption customizes a single Prequalify.Run call without affecting
// the surrounding Client.
type RunOption func(*runOptions)

// runOptions carries per-call overrides.
type runOptions struct {
	idempotencyKey string
}

// WithIdempotencyKey overrides the SDK-generated Idempotency-Key for
// one call. Useful when the caller wants the same key across an
// external retry loop.
func WithIdempotencyKey(key string) RunOption {
	return func(o *runOptions) { o.idempotencyKey = key }
}

// Run executes a prequalify request and returns the typed result.
// Validation runs locally before the request hits the wire; missing
// required fields yield a *ValidationError without a server round-trip.
func (s *PrequalifyService) Run(ctx context.Context, input *PrequalifyInput, opts ...RunOption) (*PrequalifyResult, error) {
	if err := assertPrequalifyNotV3(s.client, "Prequalify.Run"); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: PrequalifyInput is nil",
		}}
	}
	if err := input.Applicant.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: err.Error(),
		}}
	}
	if err := input.Coverage.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: err.Error(),
		}}
	}
	if input.Products.Len() == 0 {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: Products must contain at least one entry",
		}}
	}

	body, err := buildPrequalifyBody(input)
	if err != nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: err.Error(),
		}}
	}
	ro := runOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}

	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           prequalifyPath,
		body:           body,
		op:             "prequalify",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Prequalify.Run: %w", err)
	}
	return decodePrequalifyResponse(raw)
}

// RunEnvelope executes prequalify and returns the full JSON response tree.
// When ZYINS_LEGACY_WIRE=1 the request uses the engine's legacy flat-body
// shape so the live API response matches the HTTP conformance reference.
func (s *PrequalifyService) RunEnvelope(ctx context.Context, input *PrequalifyInput, opts ...RunOption) (map[string]any, error) {
	if err := assertPrequalifyNotV3(s.client, "Prequalify.RunEnvelope"); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: PrequalifyInput is nil",
		}}
	}
	if err := input.Applicant.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: err.Error(),
		}}
	}
	if err := input.Coverage.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: err.Error(),
		}}
	}
	legacy := legacyWireEnabled()
	if !legacy && input.Products.Len() == 0 {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: Products must contain at least one entry",
		}}
	}

	var wireBody any
	if legacy {
		amount := defaultLegacyFaceAmount(input.Coverage)
		wireBody = legacyPrequalifyBodyFromApplicant(input.Applicant, amount)
	} else {
		body, err := buildPrequalifyBody(input)
		if err != nil {
			return nil, &ValidationError{Base: &Error{
				Code:    ErrorCodeValidationError,
				Message: err.Error(),
			}}
		}
		wireBody = body
	}

	ro := runOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}

	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           prequalifyPath,
		body:           wireBody,
		op:             "prequalify",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Prequalify.RunEnvelope: %w", err)
	}
	return decodeJSONEnvelope(raw, "prequalify")
}

// RunWithRawResponse executes a prequalify request and returns the
// typed result alongside a RawResponse exposing the underlying HTTP
// status, headers, and URL. Mirrors the Stainless SDK convention so
// callers that need wire metadata (server-side timing headers, custom
// X-* echoes, etc.) reach for the same idiom across products.
//
// Both inputs and validation rules are identical to Run; the only
// difference is the return signature.
func (s *PrequalifyService) RunWithRawResponse(
	ctx context.Context, input *PrequalifyInput, opts ...RunOption,
) (*Envelope[*PrequalifyResult], *RawResponse, error) {
	if err := assertPrequalifyNotV3(s.client, "Prequalify.RunWithRawResponse"); err != nil {
		return nil, nil, err
	}
	if input == nil {
		return nil, nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: PrequalifyInput is nil",
		}}
	}
	if err := input.Applicant.validate(); err != nil {
		return nil, nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: err.Error(),
		}}
	}
	if err := input.Coverage.validate(); err != nil {
		return nil, nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: err.Error(),
		}}
	}
	if input.Products.Len() == 0 {
		return nil, nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: Products must contain at least one entry",
		}}
	}
	body, err := buildPrequalifyBody(input)
	if err != nil {
		return nil, nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: err.Error(),
		}}
	}
	ro := runOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}
	raw, httpResp, err := s.client.doJSONRaw(ctx, requestArgs{
		method:         http.MethodPost,
		path:           prequalifyPath,
		body:           body,
		op:             "prequalify",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, captureRawResponse(httpResp), fmt.Errorf("zyins: Prequalify.RunWithRawResponse: %w", err)
	}
	result, err := decodePrequalifyResponse(raw)
	if err != nil {
		return nil, captureRawResponse(httpResp), err
	}
	env := newEnvelope[*PrequalifyResult](result, raw, httpResp)
	return env, captureRawResponse(httpResp), nil
}

// prequalifyWireBody is the flat on-wire JSON shape for the prequalify
// request per the 0.5.1 wire contract (ADR-035). No applicant/coverage
// nesting; credentials belong in HMAC headers only.
type prequalifyWireBody struct {
	DateOfBirth   string                  `json:"date_of_birth"`
	Gender        string                  `json:"gender"`
	Height        int                     `json:"height"`
	Weight        int                     `json:"weight"`
	State         string                  `json:"state"`
	Zip           string                  `json:"zip,omitempty"`
	NicotineUsage prequalifyNicotineUsage `json:"nicotine_usage"`
	Products      []string                `json:"products"`
	Conditions    []prequalifyCondition   `json:"conditions,omitempty"`
	Medications   []prequalifyMedication  `json:"medications,omitempty"`
	QuoteOptions  prequalifyQuoteOptions  `json:"quote_options"`
}

type prequalifyNicotineUsage struct {
	LastUsed     string                 `json:"last_used"`
	ProductUsage []NicotineProductUsage `json:"product_usage,omitempty"`
}

type prequalifyCondition struct {
	Name          string `json:"name"`
	WasDiagnosed  string `json:"was_diagnosed"`
	LastTreatment string `json:"last_treatment"`
}

type prequalifyMedication struct {
	Name      string `json:"name"`
	Use       string `json:"use"`
	FirstFill string `json:"first_fill"`
	LastFill  string `json:"last_fill"`
}

type prequalifyQuoteOptions struct {
	Amounts   []string `json:"amounts"`
	QuoteType string   `json:"quote_type"`
}

// buildPrequalifyBody renders the flat wire body from the typed input.
func buildPrequalifyBody(in *PrequalifyInput) (prequalifyWireBody, error) {
	if in.Applicant.Sex != SexMale && in.Applicant.Sex != SexFemale {
		return prequalifyWireBody{}, fmt.Errorf("zyins: unknown Sex value %q", string(in.Applicant.Sex))
	}

	nicotineIn := in.Applicant.resolveNicotineUsageInput()
	nicotineWire := prequalifyNicotineUsage{LastUsed: string(nicotineIn.LastUsed)}
	if len(nicotineIn.ProductUsage) > 0 {
		nicotineWire.ProductUsage = nicotineIn.ProductUsage
	}

	conds := make([]prequalifyCondition, len(in.Applicant.Conditions))
	for i, c := range in.Applicant.Conditions {
		conds[i] = prequalifyCondition{
			Name:          c.Name,
			WasDiagnosed:  c.WasDiagnosed,
			LastTreatment: c.LastTreatment,
		}
	}

	meds := make([]prequalifyMedication, len(in.Applicant.Medications))
	for i, m := range in.Applicant.Medications {
		meds[i] = prequalifyMedication{
			Name:      m.Name,
			Use:       m.Use,
			FirstFill: m.FirstFill,
			LastFill:  m.LastFill,
		}
	}

	quoteType := "face_amounts"
	if in.Coverage.IsMonthlyBudget() {
		quoteType = "monthly_budget"
	}

	body := prequalifyWireBody{
		DateOfBirth:   in.Applicant.DOB,
		Gender:        string(in.Applicant.Sex),
		Height:        in.Applicant.Height.TotalInches,
		Weight:        in.Applicant.Weight.Pounds,
		State:         string(in.Applicant.State),
		Zip:           in.Applicant.Zip,
		NicotineUsage: nicotineWire,
		Products:      in.Products.WireTokens(),
		Conditions:    conds,
		Medications:   meds,
		QuoteOptions: prequalifyQuoteOptions{
			Amounts:   []string{fmt.Sprintf("%d", in.Coverage.Amount)},
			QuoteType: quoteType,
		},
	}
	return body, nil
}

// decodePrequalifyResponse parses the engine's JSON response. The
// server speaks the ADR-012 envelope `{ data: { plans, request_id } }`;
// fallback paths accept a flat shape for compatibility with older
// fixtures.
func decodePrequalifyResponse(body []byte) (*PrequalifyResult, error) {
	if len(body) == 0 {
		return nil, errors.New("zyins: prequalify response body was empty")
	}
	var env struct {
		Data      json.RawMessage `json:"data"`
		RequestID string          `json:"request_id"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode prequalify envelope: %w", err)
	}
	target := body
	if len(env.Data) > 0 {
		target = env.Data
	}
	var result PrequalifyResult
	if err := json.Unmarshal(target, &result); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode prequalify data: %w", err)
	}
	if result.RequestID == "" && env.RequestID != "" {
		result.RequestID = env.RequestID
	}
	return &result, nil
}
