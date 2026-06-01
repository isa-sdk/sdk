package zyins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// quotePath is the canonical path for the quote operation.
const quotePath = "/v1/quote"

// legacyQuotePath is the engine path used by the flat legacy conformance body.
const legacyQuotePath = "/v2/quote"

const (
	quoteSexMale   = "M"
	quoteSexFemale = "F"
)

// QuoteService is the typed sub-service for the quote operation.
// Quote finalizes a premium for one chosen plan after a prequalify run
// has selected the carrier/tier combination.
type QuoteService struct {
	client *Client
}

// QuoteInput is the typed request shape for Quote.Run. Constructed by
// the caller as a struct literal; Run validates required fields before
// the request hits the wire.
type QuoteInput struct {
	// Applicant is the underwriting profile; same shape as prequalify.
	Applicant Applicant
	// Coverage is the requested coverage shape.
	Coverage Coverage
	// ProductToken is the engine-canonical token for the chosen plan,
	// typically copied from PrequalifyPlan.ProductToken.
	ProductToken string
}

// QuoteResult is the typed response shape for Quote.Run.
//
// Money fields are integer minor units (USD cents). Float64 cannot
// represent every dollar-and-cent value exactly; rendering a UI string
// MUST format from cents (divide by 100, pad to two decimals).
type QuoteResult struct {
	// MonthlyPremiumCents is the final monthly premium in USD cents.
	MonthlyPremiumCents int64 `json:"monthly_premium_cents"`
	// FaceValueCents is the death benefit the premium applies to, in
	// USD cents.
	FaceValueCents int64 `json:"face_value_cents"`
	// EffectiveDate is the policy effective date as an ISO 8601 string.
	EffectiveDate string `json:"effective_date,omitempty"`
	// QuoteID is the server's correlation identifier; preserve it when
	// routing the quote into an eApp submission.
	QuoteID string `json:"quote_id"`
	// RequestID is the server-side request correlator.
	RequestID string `json:"request_id"`
}

// Run executes a quote request and returns the typed result.
func (s *QuoteService) Run(ctx context.Context, input *QuoteInput, opts ...RunOption) (*QuoteResult, error) {
	if err := assertQuoteNotV3(s.client, "Quote.Run"); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: QuoteInput is nil",
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
	if input.ProductToken == "" {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: QuoteInput.ProductToken is required",
		}}
	}

	body := buildQuoteBody(input)
	ro := runOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}

	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           quotePath,
		body:           body,
		op:             "quote",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Quote.Run: %w", err)
	}
	return decodeQuoteResponse(raw)
}

// RunEnvelope executes quote and returns the full JSON response tree.
// When ZYINS_LEGACY_WIRE=1 the request uses the engine's legacy flat-body
// shape so the live API response matches the HTTP conformance reference.
func (s *QuoteService) RunEnvelope(ctx context.Context, input *QuoteInput, opts ...RunOption) (map[string]any, error) {
	if err := assertQuoteNotV3(s.client, "Quote.RunEnvelope"); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: QuoteInput is nil",
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

	var wireBody any
	path := quotePath
	if legacyWireEnabled() {
		amount := defaultLegacyFaceAmount(input.Coverage)
		wireBody = legacyQuoteBodyFromApplicant(input.Applicant, amount)
		path = legacyQuotePath
	} else if input.ProductToken == "" {
		return nil, &ValidationError{Base: &Error{
			Code:    ErrorCodeValidationError,
			Message: "zyins: QuoteInput.ProductToken is required",
		}}
	} else {
		wireBody = buildQuoteBody(input)
	}

	ro := runOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}

	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           path,
		body:           wireBody,
		op:             "quote",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Quote.RunEnvelope: %w", err)
	}
	return decodeJSONEnvelope(raw, "quote")
}

// RunWithRawResponse executes a quote request and returns the typed
// result alongside a RawResponse exposing the underlying HTTP status,
// headers, and URL. Mirrors PrequalifyService.RunWithRawResponse.
func (s *QuoteService) RunWithRawResponse(
	ctx context.Context, input *QuoteInput, opts ...RunOption,
) (*Envelope[*QuoteResult], *RawResponse, error) {
	if err := assertQuoteNotV3(s.client, "Quote.RunWithRawResponse"); err != nil {
		return nil, nil, err
	}
	if input == nil {
		return nil, nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: QuoteInput is nil",
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
	if input.ProductToken == "" {
		return nil, nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: QuoteInput.ProductToken is required",
		}}
	}
	body := buildQuoteBody(input)
	ro := runOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}
	raw, httpResp, err := s.client.doJSONRaw(ctx, requestArgs{
		method:         http.MethodPost,
		path:           quotePath,
		body:           body,
		op:             "quote",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, captureRawResponse(httpResp), fmt.Errorf("zyins: Quote.RunWithRawResponse: %w", err)
	}
	result, err := decodeQuoteResponse(raw)
	if err != nil {
		return nil, captureRawResponse(httpResp), err
	}
	env := newEnvelope[*QuoteResult](result, raw, httpResp)
	return env, captureRawResponse(httpResp), nil
}

// quoteWireBody is the on-wire JSON shape for the quote request.
type quoteWireBody struct {
	Applicant    quoteWireApplicant `json:"applicant"`
	Coverage     Coverage           `json:"coverage"`
	ProductToken string             `json:"product_token"`
}

// quoteWireApplicant is the applicant sub-object for the quote endpoint.
type quoteWireApplicant struct {
	DOB          string        `json:"dob"`
	Sex          string        `json:"sex"`
	HeightInches int           `json:"height_inches"`
	WeightPounds int           `json:"weight_pounds"`
	State        string        `json:"state"`
	Zip          string        `json:"zip,omitempty"`
	NicotineUse  NicotineUsage `json:"nicotine_use"`
	Medications  []Medication  `json:"medications,omitempty"`
	Conditions   []Condition   `json:"conditions,omitempty"`
}

// buildQuoteBody renders the wire body for the quote endpoint.
func buildQuoteBody(input *QuoteInput) quoteWireBody {
	return quoteWireBody{
		Applicant: quoteWireApplicant{
			DOB:          input.Applicant.DOB,
			Sex:          quoteSexWireCode(input.Applicant.Sex),
			HeightInches: input.Applicant.Height.TotalInches,
			WeightPounds: input.Applicant.Weight.Pounds,
			State:        string(input.Applicant.State),
			Zip:          input.Applicant.Zip,
			NicotineUse:  quoteNicotineUsage(input.Applicant.resolveNicotineUsageInput()),
			Medications:  input.Applicant.Medications,
			Conditions:   input.Applicant.Conditions,
		},
		Coverage:     input.Coverage,
		ProductToken: input.ProductToken,
	}
}

func quoteSexWireCode(sex Sex) string {
	switch sex {
	case SexMale:
		return quoteSexMale
	case SexFemale:
		return quoteSexFemale
	default:
		return ""
	}
}

func quoteNicotineUsage(usage NicotineUsageInput) NicotineUsage {
	switch usage.LastUsed {
	case NicotineNever:
		return NicotineNone
	case NicotineWithin12Months:
		return NicotineCurrent
	default:
		return NicotineFormer
	}
}

// decodeQuoteResponse parses the server response, unwrapping the
// ADR-012 envelope when present.
func decodeQuoteResponse(body []byte) (*QuoteResult, error) {
	if len(body) == 0 {
		return nil, errors.New("zyins: quote response body was empty")
	}
	var env struct {
		Data      json.RawMessage `json:"data"`
		RequestID string          `json:"request_id"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode quote envelope: %w", err)
	}
	target := body
	if len(env.Data) > 0 {
		target = env.Data
	}
	var result QuoteResult
	if err := json.Unmarshal(target, &result); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode quote data: %w", err)
	}
	if result.RequestID == "" && env.RequestID != "" {
		result.RequestID = env.RequestID
	}
	return &result, nil
}
