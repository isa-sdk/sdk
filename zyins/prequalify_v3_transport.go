// Package zyins — v3 prequalify + quote transport.
//
// Mirrors the TS bindings in packages/ts/src/zyins/prequalify-v3.ts and
// quote-v3.ts: builds the flat wire body, mints a UUID v4 idempotency
// key when the caller doesn't supply one, posts to /v3/{prequalify,quote},
// and parses the typed pricing[] envelope.

package zyins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	prequalifyV3Path = "/v3/prequalify"
	quoteV3Path      = "/v3/quote"

	// apiVersionHeader pins the request to a specific API version even
	// when a transport-layer middleware mutates the URL. The v3
	// prequalify envelope handler keys off this header on the server.
	apiVersionHeader = "Api-Version"

	// centsPerDollar converts whole-dollar SDK amounts to the integer
	// cents the v3 coverage envelope requires.
	centsPerDollar = 100

	// v3NicotineDefaultFrequency is the wire fallback when the caller
	// supplies a NicotineProductUsage.Frequency the v3 enum does not
	// model.
	v3NicotineDefaultFrequency = "daily"
)

// v3NicotineFrequency maps SDK-grade frequency strings (which still
// carry the v2 DAILY/WEEKLY/MONTHLY/YEARLY vocabulary) to the v3
// NicotineFrequencyV3 enum the server accepts on /v3/prequalify.
// Unknown values fall back to v3NicotineDefaultFrequency at the call
// site so a v3 caller never needs to know the wire enum names.
var v3NicotineFrequency = map[string]string{
	"daily":               "daily",
	"DAILY":               "daily",
	"weekly":              "few_times_per_week",
	"WEEKLY":              "few_times_per_week",
	"few_times_per_week":  "few_times_per_week",
	"monthly":             "few_times_per_month",
	"MONTHLY":             "few_times_per_month",
	"few_times_per_month": "few_times_per_month",
	"yearly":              "few_times_per_year",
	"YEARLY":              "few_times_per_year",
	"few_times_per_year":  "few_times_per_year",
}

// V3RunOption customizes a single Run call without affecting the
// surrounding Client. Mirrors RunOption on the v1 surface; kept
// separate because the v3 idempotency contract requires UUID v4, while
// the v1 surface accepts opaque keys.
type V3RunOption func(*v3RunOptions)

type v3RunOptions struct {
	idempotencyKey string
}

// WithV3IdempotencyKey overrides the SDK-generated key for one call.
// The caller is responsible for supplying a UUID v4 — the v3 contract
// rejects anything else with idempotency_conflict.
func WithV3IdempotencyKey(key string) V3RunOption {
	return func(o *v3RunOptions) { o.idempotencyKey = key }
}

// Run executes a v3 prequalify request.
//
// Returns *ValidationError before hitting the wire when applicant or
// coverage validation fails; mirrors the v1 Run contract so callers
// branch on the same typed error across surfaces.
func (s *PrequalifyV3Service) Run(ctx context.Context, req *PrequalifyV3Request, opts ...V3RunOption) (*PrequalifyV3Result, error) {
	if err := assertPrequalifyAPIVersion(s.client, apiVersionV3, "PrequalifyV3.Run"); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: PrequalifyV3Request is nil",
		}}
	}
	body, err := buildV3PrequalifyEnvelopeBody(req.Applicant, req.Coverage, req.Products, req.Options)
	if err != nil {
		return nil, err
	}
	ro := v3RunOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}
	key := ro.idempotencyKey
	if key == "" {
		key, err = mintUUIDv4()
		if err != nil {
			return nil, fmt.Errorf("zyins: PrequalifyV3.Run mint idempotency key: %w", err)
		}
	}
	raw, httpResp, err := s.client.doJSONRaw(ctx, requestArgs{
		method:         http.MethodPost,
		path:           prequalifyV3Path,
		body:           json.RawMessage(body),
		op:             "prequalify_v3",
		idempotencyKey: key,
		extraHeaders:   map[string]string{apiVersionHeader: apiVersionV3},
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: PrequalifyV3.Run: %w", err)
	}
	result, err := decodePrequalifyV3Envelope(raw, key)
	if err != nil {
		return nil, fmt.Errorf("zyins: PrequalifyV3.Run decode: %w", err)
	}
	if httpResp != nil {
		// Reserved for future header-derived fields (retry attempts,
		// rate-limit metadata). The base envelope carries everything
		// the TS surface returns today.
		_ = httpResp
	}
	return result, nil
}

// Run executes a v3 quote request.
func (s *QuoteV3Service) Run(ctx context.Context, req *QuoteV3Request, opts ...V3RunOption) (*QuoteV3Result, error) {
	if err := assertQuoteAPIVersion(s.client, apiVersionV3, "QuoteV3.Run"); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: QuoteV3Request is nil",
		}}
	}
	body, err := buildV3WireBody(req.Applicant, req.Coverage, req.Products, req.Options)
	if err != nil {
		return nil, err
	}
	ro := v3RunOptions{}
	for _, o := range opts {
		if o != nil {
			o(&ro)
		}
	}
	key := ro.idempotencyKey
	if key == "" {
		key, err = mintUUIDv4()
		if err != nil {
			return nil, fmt.Errorf("zyins: QuoteV3.Run mint idempotency key: %w", err)
		}
	}
	raw, _, err := s.client.doJSONRaw(ctx, requestArgs{
		method:         http.MethodPost,
		path:           quoteV3Path,
		body:           json.RawMessage(body),
		op:             "quote_v3",
		idempotencyKey: key,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: QuoteV3.Run: %w", err)
	}
	result, err := decodeQuoteV3Envelope(raw, key)
	if err != nil {
		return nil, fmt.Errorf("zyins: QuoteV3.Run decode: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Wire body — v3 prequalify envelope shape.
//
// `POST /v3/prequalify` consumes the envelope `PrequalifyV3Request`
// schema (`applicant` + `coverage` + `products[]`) — NOT the v2 flat
// shape that `/v3/quote` still consumes via buildV3WireBody below.
// Emitting the v2 flat shape against /v3/prequalify produces
// `unknown field "date_of_birth"` from the zyins server (prod
// incident, 2026-05-29 — see PR #406 for the parallel TS fix).
//
// Canonical schemas: PrequalifyV3Request / ApplicantV3Input /
// CoverageV3Input / ConditionV3Input / MedicationV3Input /
// NicotineUsageInput in go/zyins/api/openapi.yaml.
// ---------------------------------------------------------------------------

// buildV3PrequalifyEnvelopeBody serializes a PrequalifyV3Request into
// the `PrequalifyV3Request` wire envelope.
//
// `applicant.state` and `applicant.zip` move into the coverage envelope
// per the v3 schema (zip is required for medsup quotes; the server
// zip-gates and silently filters medsup products when it is absent).
// `options.MinRank`, `options.ShowUnreleased`,
// `options.SkipHealthBasedUnderwriting`, `options.OnlyProductClass`,
// `options.IncludeProductClass` are not part of the v3 prequalify
// envelope and are silently dropped — they survive on /v3/quote via
// the legacy flat body. v3 prequalify is face-amount-only; multi-amount
// and monthly-budget callers must use QuoteV3.Run.
func buildV3PrequalifyEnvelopeBody(app Applicant, cov Coverage, products ProductSelection, options *PrequalifyV3Options) ([]byte, error) {
	if err := app.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: err.Error(),
		}}
	}
	if err := cov.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: err.Error(),
		}}
	}
	if products.Len() == 0 {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: Products must contain at least one entry",
		}}
	}

	applicant := map[string]any{
		"sex":           string(app.Sex),
		"dob":           app.DOB,
		"height_inches": app.Height.TotalInches,
		"weight_lbs":    app.Weight.Pounds,
		"nicotine":      buildV3Nicotine(app.resolveNicotineUsageInput()),
	}
	if len(app.Conditions) > 0 {
		applicant["conditions"] = buildV3Conditions(app.Conditions)
	}
	if len(app.Medications) > 0 {
		applicant["medications"] = buildV3Medications(app.Medications)
	}

	payload := map[string]any{
		"applicant": applicant,
		"coverage":  buildV3Coverage(cov, string(app.State), app.Zip),
		"products":  products.WireTokens(),
	}
	if options != nil && options.IncludeIneligible != nil {
		payload["include_ineligible"] = *options.IncludeIneligible
	} else {
		// TS surface defaults to true so consumers always see the full
		// pricing[] table; preserve cross-SDK byte parity.
		payload["include_ineligible"] = true
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("zyins: failed to serialize v3 prequalify envelope: %w", err)
	}
	return out, nil
}

// buildV3Coverage serializes the coverage envelope from the input shape.
//
// A single face amount keeps the proven {face_amount_cents} wire shape
// (integer cents). A multi-amount probe mirrors the /v3/quote
// quote_options block — {quote_type, amounts: []string} — so the server's
// additive face_amount_cents XOR quote_options contract (zyins #400) is
// satisfied with one serializer per shape. state and zip ride the
// envelope in every case; zip is omitted when empty (the server pattern
// ^\d{5}(-\d{4})?$ rejects an empty string and zip is required only for
// medsup quotes).
func buildV3Coverage(cov Coverage, state, zip string) map[string]any {
	withLocale := func(coverage map[string]any) map[string]any {
		coverage["state"] = state
		if zip != "" {
			coverage["zip"] = zip
		}
		return coverage
	}
	if cov.IsMulti() {
		amounts := make([]string, len(cov.Amounts))
		for i, a := range cov.Amounts {
			amounts[i] = fmt.Sprintf("%d", a)
		}
		return withLocale(map[string]any{
			"quote_options": map[string]any{
				"quote_type": v3QuoteType(cov),
				"amounts":    amounts,
			},
		})
	}
	// A single monthly budget has no face_amount_cents to express, so it
	// rides the quote_options block with one amount — the same path the
	// server (zyins #400) accepts for the multi-amount budget probe. A
	// single face amount keeps the proven face_amount_cents wire shape.
	if cov.IsMonthlyBudget() {
		return withLocale(map[string]any{
			"quote_options": map[string]any{
				"quote_type": "monthly_budget",
				"amounts":    []string{fmt.Sprintf("%d", cov.Amount)},
			},
		})
	}
	return withLocale(map[string]any{
		"face_amount_cents": cov.Amount * centsPerDollar,
	})
}

// v3QuoteType maps a multi-amount coverage to its quote_options
// discriminator.
func v3QuoteType(cov Coverage) string {
	if cov.IsMonthlyBudget() {
		return "monthly_budget"
	}
	return "face_amounts"
}

// buildV3Conditions maps SDK Condition rows to the ConditionV3Input
// wire shape: `name` → `text`, with optional `was_diagnosed` and
// `last_treatment` passed through verbatim (the engine accepts ISO 8601,
// US format, and relative phrasing).
func buildV3Conditions(conds []Condition) []map[string]any {
	rows := make([]map[string]any, 0, len(conds))
	for _, c := range conds {
		row := map[string]any{"text": c.Name}
		if c.WasDiagnosed != "" {
			row["was_diagnosed"] = c.WasDiagnosed
		}
		if c.LastTreatment != "" {
			row["last_treatment"] = c.LastTreatment
		}
		rows = append(rows, row)
	}
	return rows
}

// buildV3Medications maps SDK Medication rows to the MedicationV3Input
// wire shape: `name` → `text`, with `use`, `first_fill`, `last_fill`
// passed through verbatim.
func buildV3Medications(meds []Medication) []map[string]any {
	rows := make([]map[string]any, 0, len(meds))
	for _, m := range meds {
		row := map[string]any{"text": m.Name}
		if m.Use != "" {
			row["use"] = m.Use
		}
		if m.FirstFill != "" {
			row["first_fill"] = m.FirstFill
		}
		if m.LastFill != "" {
			row["last_fill"] = m.LastFill
		}
		rows = append(rows, row)
	}
	return rows
}

// buildV3Nicotine maps the SDK NicotineUsageInput to the v3
// NicotineUsageInput wire shape: `{ last_used, specificity[] }`.
// Product rows surface as NicotineSpecificityInput entries with the
// freeform name as `text` and frequency coerced to the
// NicotineFrequencyV3 enum via v3NicotineFrequency.
func buildV3Nicotine(in NicotineUsageInput) map[string]any {
	out := map[string]any{
		"last_used": string(in.LastUsed),
	}
	if len(in.ProductUsage) > 0 {
		spec := make([]map[string]any, 0, len(in.ProductUsage))
		for _, p := range in.ProductUsage {
			freq, ok := v3NicotineFrequency[p.Frequency]
			if !ok {
				freq = v3NicotineDefaultFrequency
			}
			spec = append(spec, map[string]any{
				"text":      p.Type,
				"frequency": freq,
			})
		}
		out["specificity"] = spec
	}
	return out
}

// ---------------------------------------------------------------------------
// Wire body — v3 quote (legacy flat shape).
//
// `POST /v3/quote` currently consumes the v2 QuoteRequest flat body
// (see openapi.yaml operation `quoteV3`). Shared here as the
// serializer until /v3/quote migrates to its own envelope.
// ---------------------------------------------------------------------------

func buildV3WireBody(app Applicant, cov Coverage, products ProductSelection, options *PrequalifyV3Options) ([]byte, error) {
	if err := app.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: err.Error(),
		}}
	}
	if err := cov.validate(); err != nil {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: err.Error(),
		}}
	}
	if products.Len() == 0 {
		return nil, &ValidationError{Base: &Error{
			Code: ErrorCodeValidationError, Message: "zyins: Products must contain at least one entry",
		}}
	}

	nicotineIn := app.resolveNicotineUsageInput()
	nicotine := map[string]any{
		"last_used": string(nicotineIn.LastUsed),
	}
	if len(nicotineIn.ProductUsage) > 0 {
		nicotine["product_usage"] = nicotineIn.ProductUsage
	}

	quoteType := "face_amounts"
	if cov.IsMonthlyBudget() {
		quoteType = "monthly_budget"
	}

	conds := app.Conditions
	if conds == nil {
		conds = []Condition{}
	}
	meds := app.Medications
	if meds == nil {
		meds = []Medication{}
	}

	payload := map[string]any{
		"date_of_birth":  app.DOB,
		"gender":         string(app.Sex),
		"height":         app.Height.TotalInches,
		"weight":         app.Weight.Pounds,
		"state":          string(app.State),
		"nicotine_usage": nicotine,
		"conditions":     conds,
		"medications":    meds,
		"quote_options": map[string]any{
			"quote_type": quoteType,
			"amounts":    []string{fmt.Sprintf("%d", cov.Amount)},
		},
		"products": products.WireTokens(),
	}
	if app.Zip != "" {
		payload["zip"] = app.Zip
	}
	if options != nil {
		if options.OnlyProductClass != "" {
			payload["only_product_class"] = options.OnlyProductClass
		}
		if len(options.IncludeProductClass) > 0 {
			payload["include_product_class"] = options.IncludeProductClass
		}
		if options.MinRank != "" {
			payload["min_rank"] = options.MinRank
		}
		if options.ShowUnreleased != nil {
			payload["show_unreleased"] = *options.ShowUnreleased
		}
		if options.SkipHealthBasedUnderwriting != nil {
			payload["skip_health_based_underwriting"] = *options.SkipHealthBasedUnderwriting
		}
		if options.IncludeIneligible != nil {
			payload["include_ineligible"] = *options.IncludeIneligible
		}
	}
	if _, present := payload["include_ineligible"]; !present {
		// TS defaults to true so the consumer always sees the full
		// pricing[] table; preserve byte parity.
		payload["include_ineligible"] = true
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("zyins: failed to serialize v3 request body: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// UUID v4 minting.
//
// The v3 contract requires UUID v4 in Idempotency-Key (api-standards
// §Idempotency). crypto/rand fails closed — callers see a typed error
// rather than the SDK silently falling back to a weaker generator.
// ---------------------------------------------------------------------------

func mintUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("zyins: idempotency key entropy: %w", err)
	}
	// RFC 4122 §4.4: set version + variant bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hex16 := hex.EncodeToString(b[:])
	return hex16[0:8] + "-" + hex16[8:12] + "-" + hex16[12:16] + "-" + hex16[16:20] + "-" + hex16[20:32], nil
}

// ---------------------------------------------------------------------------
// Response decode.
//
// The v3 envelope shape is `{ object, request_id, idempotency_key,
// livemode, data: { plans | results } }`. The data block carries the
// uniform pricing[] table per offer.
// ---------------------------------------------------------------------------

type v3EnvelopeHeader struct {
	RequestID      string          `json:"request_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Livemode       *bool           `json:"livemode"`
	Data           json.RawMessage `json:"data"`
}

func decodePrequalifyV3Envelope(body []byte, sentKey string) (*PrequalifyV3Result, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("zyins: prequalify_v3 response body was empty")
	}
	var env v3EnvelopeHeader
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode prequalify_v3 envelope: %w", err)
	}
	if len(env.Data) == 0 || isJSONNull(env.Data) {
		return nil, fmt.Errorf("zyins: prequalify_v3 envelope missing data field")
	}
	echoKey := env.IdempotencyKey
	if echoKey == "" {
		echoKey = sentKey
	}
	livemode := false
	if env.Livemode != nil {
		livemode = *env.Livemode
	}
	plans, err := decodeV3Plans(env.Data)
	if err != nil {
		return nil, err
	}
	return &PrequalifyV3Result{
		Plans:          plans,
		RequestID:      env.RequestID,
		IdempotencyKey: echoKey,
		Livemode:       livemode,
	}, nil
}

// decodeV3Plans decodes the flat `plans[]` block shared by the
// /v3/prequalify and /v3/quote envelopes into typed offers.
// Returns an error if the `plans` key is absent from the response
// (vs present-but-empty, which is a valid no-offers result).
func decodeV3Plans(data json.RawMessage) ([]V3Offer, error) {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode v3 plans: %w", err)
	}
	plansRaw, hasPlans := parsed["plans"]
	if !hasPlans {
		return nil, fmt.Errorf("zyins: missing plans field in v3 response")
	}
	var plansList []json.RawMessage
	if err := json.Unmarshal(plansRaw, &plansList); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode v3 plans array: %w", err)
	}
	plans := make([]V3Offer, 0, len(plansList))
	for _, raw := range plansList {
		offer, err := decodeV3Offer(raw)
		if err != nil {
			return nil, err
		}
		plans = append(plans, offer)
	}
	return plans, nil
}

func decodeQuoteV3Envelope(body []byte, sentKey string) (*QuoteV3Result, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("zyins: quote_v3 response body was empty")
	}
	var env v3EnvelopeHeader
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode quote_v3 envelope: %w", err)
	}
	if len(env.Data) == 0 || isJSONNull(env.Data) {
		return nil, fmt.Errorf("zyins: quote_v3 envelope missing data field")
	}
	plans, err := decodeV3Plans(env.Data)
	if err != nil {
		return nil, err
	}
	echoKey := env.IdempotencyKey
	if echoKey == "" {
		echoKey = sentKey
	}
	livemode := false
	if env.Livemode != nil {
		livemode = *env.Livemode
	}
	return &QuoteV3Result{
		Plans:          plans,
		RequestID:      env.RequestID,
		IdempotencyKey: echoKey,
		Livemode:       livemode,
	}, nil
}

// ---------------------------------------------------------------------------
// Offer / product / pricing decode.
//
// Defensive: missing fields decode to zero values rather than erroring,
// matching the TS coercion behavior. Wire fields the SDK doesn't model
// pass through `metadata`.
// ---------------------------------------------------------------------------

type v3WireMoney struct {
	Amount V3Amount `json:"amount"`
	Period *string  `json:"period"`
}

type v3WireOffer struct {
	ID           string            `json:"id"`
	Eligible     bool              `json:"eligible"`
	Carrier      V3OfferCarrier    `json:"carrier"`
	Product      V3OfferProduct    `json:"product"`
	PlanInfo     json.RawMessage   `json:"plan_info"`
	DeathBenefit *v3WireMoney      `json:"death_benefit"`
	Budget       *v3WireMoney      `json:"budget"`
	Pricing      []json.RawMessage `json:"pricing"`
	Metadata     map[string]any    `json:"metadata"`
}

// coerceV3Money builds a typed V3Money, dropping any period value outside
// the closed enum so an unknown future period never poisons the type.
func coerceV3Money(w v3WireMoney) V3Money {
	m := V3Money{Amount: w.Amount}
	if w.Period != nil {
		switch p := V3Period(*w.Period); p {
		case V3PeriodMonthly, V3PeriodQuarterly, V3PeriodSemiannual, V3PeriodAnnual:
			m.Period = &p
		}
	}
	return m
}

func decodeV3Offer(raw json.RawMessage) (V3Offer, error) {
	var w v3WireOffer
	if err := json.Unmarshal(raw, &w); err != nil {
		return V3Offer{}, fmt.Errorf("zyins: failed to decode v3 offer: %w", err)
	}
	pricing := make([]V3PricingRow, 0, len(w.Pricing))
	for _, p := range w.Pricing {
		row, err := decodeV3PricingRow(p)
		if err != nil {
			return V3Offer{}, err
		}
		pricing = append(pricing, row)
	}
	var planInfoTyped []PlanInfoItem
	if len(w.PlanInfo) > 0 && !isJSONNull(w.PlanInfo) {
		var anyVal any
		if err := json.Unmarshal(w.PlanInfo, &anyVal); err == nil {
			planInfoTyped = CoercePlanInfo(anyVal)
		}
	}
	metadata := w.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	offer := V3Offer{
		Object:   "plan_offer",
		ID:       w.ID,
		Eligible: w.Eligible,
		Carrier:  w.Carrier,
		Product:  w.Product,
		PlanInfo: planInfoTyped,
		Pricing:  pricing,
		Metadata: metadata,
	}
	// death_benefit is null on the wire for premium-only products (medsup); a
	// Money object for life products. Preserve nil rather than coercing it into
	// a zero-cents Money, so consumers nil-check medsup correctly.
	if w.DeathBenefit != nil {
		deathBenefit := coerceV3Money(*w.DeathBenefit)
		offer.DeathBenefit = &deathBenefit
	}
	if w.Budget != nil {
		budget := coerceV3Money(*w.Budget)
		offer.Budget = &budget
	}
	return offer, nil
}

type v3WirePricingRow struct {
	RateClass   string            `json:"rate_class"`
	Primary     bool              `json:"primary"`
	Eligibility v3WireEligibility `json:"eligibility"`
	Premium     *v3WirePremium    `json:"premium"`
	Rank        *int              `json:"rank"`
}

type v3WireEligibility struct {
	Category *string  `json:"category"`
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons"`
}

type v3WirePremium struct {
	Amount      *V3Amount           `json:"amount"`
	DefaultMode string              `json:"default_mode"`
	Modes       map[string]V3Amount `json:"modes"`
}

func decodeV3PricingRow(raw json.RawMessage) (V3PricingRow, error) {
	var w v3WirePricingRow
	if err := json.Unmarshal(raw, &w); err != nil {
		return V3PricingRow{}, fmt.Errorf("zyins: failed to decode v3 pricing row: %w", err)
	}
	row := V3PricingRow{
		RateClass: w.RateClass,
		Primary:   w.Primary,
		Eligibility: V3Eligibility{
			Eligible: w.Eligibility.Eligible,
			Reasons:  w.Eligibility.Reasons,
		},
		Rank: w.Rank,
	}
	if w.Eligibility.Category != nil {
		cat := V3EligibilityCategory(*w.Eligibility.Category)
		switch cat {
		case V3EligibilityCategoryImmediate, V3EligibilityCategoryGraded, V3EligibilityCategoryROP, V3EligibilityCategoryOther:
			row.Eligibility.Category = &cat
		default:
			// Unknown future category — drop rather than poison the
			// typed enum. The TS surface does the same.
		}
	}
	if row.Eligibility.Reasons == nil {
		row.Eligibility.Reasons = []string{}
	}
	if w.Premium != nil {
		modes := w.Premium.Modes
		if modes == nil {
			modes = map[string]V3Amount{}
		}
		// Amount is byte-identical to Modes[DefaultMode]; read the wire amount
		// when present, else fall back to the mode grid so a server that only
		// populated Modes still yields the headline value.
		var amount V3Amount
		switch {
		case w.Premium.Amount != nil:
			amount = *w.Premium.Amount
		default:
			amount = modes[w.Premium.DefaultMode]
		}
		row.Premium = &V3Premium{
			Amount:      amount,
			DefaultMode: w.Premium.DefaultMode,
			Modes:       modes,
		}
	}
	return row, nil
}
