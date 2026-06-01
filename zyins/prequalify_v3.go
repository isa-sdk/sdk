// Package zyins — v3 prequalify + quote stubs.
//
// Declares the v3 uniform pricing[] shape the TS SDK ships. The Go
// transport implementation is a follow-up; types land first so
// consumers can compile against the public surface.

package zyins

// V3OfferCarrier is the carrier underwriting an offer. Declared here
// because the existing Go SDK predates the typed offer surface; once
// the v2 transformer migration lands the type moves to its own file.
type V3OfferCarrier struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	LogoURL string `json:"logo_url"`
}

// V3OfferProduct is the carrier product an offer represents.
type V3OfferProduct struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	WireToken   string `json:"wire_token"`
}

// V3EligibilityCategory is the underwriting rank bucket — the axis
// min_rank filters on. NOT the carrier rate-class label.
type V3EligibilityCategory string

const (
	V3EligibilityCategoryImmediate V3EligibilityCategory = "immediate"
	V3EligibilityCategoryGraded    V3EligibilityCategory = "graded"
	V3EligibilityCategoryROP       V3EligibilityCategory = "rop"
	V3EligibilityCategoryOther     V3EligibilityCategory = "other"
)

// V3Eligibility describes whether one pricing row qualifies, and the
// generic carrier-confidential reasons populated when it does not.
type V3Eligibility struct {
	Category *V3EligibilityCategory `json:"category"`
	Eligible bool                   `json:"eligible"`
	Reasons  []string               `json:"reasons"`
}

// V3Amount is an integer-cents value paired with the carrier display
// string (the OpenAPI AmountResponse). Cents is canonical for arithmetic;
// Display renders verbatim and is never parsed.
type V3Amount struct {
	Cents   int64  `json:"cents"`
	Display string `json:"display"`
}

// V3Period is the recurrence period for a V3Money. The empty string ("",
// serialized as JSON null) is a one-time / lump-sum amount (a death
// benefit); the named values are premium billing cycles.
type V3Period string

const (
	V3PeriodMonthly    V3Period = "monthly"
	V3PeriodQuarterly  V3Period = "quarterly"
	V3PeriodSemiannual V3Period = "semiannual"
	V3PeriodAnnual     V3Period = "annual"
)

// V3Money is a monetary value with a recurrence period (the OpenAPI
// Money). Used for DeathBenefit (Period "" / null, a one-time lump sum)
// and Budget (Period "monthly", the requested monthly budget). Amount is
// the canonical V3Amount; Period disambiguates one-time vs recurring.
type V3Money struct {
	Amount V3Amount  `json:"amount"`
	Period *V3Period `json:"period"`
}

// V3Premium is the premium for one pricing row. Amount is the headline
// value clients compare across carriers; it is byte-identical to
// Modes[DefaultMode]. DefaultMode names which Modes entry Amount was drawn
// from — the carrier mode token (MONTHLY-EFT, ANNUAL, ...), which itself
// encodes the recurrence, so premium carries no period field.
type V3Premium struct {
	Amount      V3Amount            `json:"amount"`
	DefaultMode string              `json:"default_mode"`
	Modes       map[string]V3Amount `json:"modes"`
}

// V3PricingRow is one row of the uniform pricing[] table — a single
// rate class for one product.
type V3PricingRow struct {
	RateClass   string        `json:"rate_class"`
	Primary     bool          `json:"primary"`
	Eligibility V3Eligibility `json:"eligibility"`
	Premium     *V3Premium    `json:"premium,omitempty"`
	Rank        *int          `json:"rank"`
}

// V3Offer is one product's offer, returned identically by /v3/prequalify
// and /v3/quote. DeathBenefit is non-nil for life products (fex/term/preneed)
// as a one-time lump sum (Period nil); it is nil for premium-only products
// (medsup), whose coverage value lives entirely in Pricing[].Premium. Budget
// is present only on monthly-budget quotes (Period "monthly", the requested
// budget — the stable grouping key for budget responses). Array order of
// Pricing is authoritative for display.
type V3Offer struct {
	Object       string         `json:"object"`
	ID           string         `json:"id"`
	Eligible     bool           `json:"eligible"`
	Carrier      V3OfferCarrier `json:"carrier"`
	Product      V3OfferProduct `json:"product"`
	PlanInfo     []PlanInfoItem `json:"plan_info"`
	DeathBenefit *V3Money       `json:"death_benefit"`
	Budget       *V3Money       `json:"budget,omitempty"`
	Pricing      []V3PricingRow `json:"pricing"`
	Metadata     map[string]any `json:"metadata"`
}

// PrequalifyV3Result is the payload of the v3 prequalify envelope's data
// field. Always a flat Plans slice — single amount and multi-amount alike.
// Group client-side by the requested dimension with ByAmount (DeathBenefit
// for face-amount requests, Budget for monthly-budget requests); the shape
// never changes with the amount count.
type PrequalifyV3Result struct {
	Plans          []V3Offer `json:"plans"`
	RequestID      string    `json:"-"`
	IdempotencyKey string    `json:"-"`
	Livemode       bool      `json:"-"`
	RetryAttempts  int       `json:"-"`
}

// QuoteV3Result is the payload of the v3 quote envelope's data field —
// the identical flat Plans shape as PrequalifyV3Result.
type QuoteV3Result struct {
	Plans          []V3Offer `json:"plans"`
	RequestID      string    `json:"-"`
	IdempotencyKey string    `json:"-"`
	Livemode       bool      `json:"-"`
	RetryAttempts  int       `json:"-"`
}

// ByAmount groups a flat plans slice by the requested coverage dimension.
// When any offer carries a Budget (a monthly-budget response) the offers
// key off Budget.Amount.Cents; otherwise off DeathBenefit.Amount.Cents (a
// face-amount response). The returned map's grouping keys are integer
// cents; iterate the original plans for display order.
//
// In budget mode, an offer missing Budget is skipped (contract violation)
// rather than falling back to DeathBenefit, which would mis-bucket mixed
// offers. In face-amount mode, an offer with a nil DeathBenefit (a medsup
// product, which has no face amount) is likewise skipped — it has no
// face-amount dimension to group on.
func ByAmount(plans []V3Offer) map[int64][]V3Offer {
	isBudget := false
	for i := range plans {
		if plans[i].Budget != nil {
			isBudget = true
			break
		}
	}
	grouped := make(map[int64][]V3Offer, len(plans))
	for _, offer := range plans {
		var dimension *V3Money
		if isBudget {
			dimension = offer.Budget
		} else {
			dimension = offer.DeathBenefit
		}
		// Budget mode: missing budget is a contract violation. Face-amount
		// mode: a nil death_benefit is a medsup product with no face-amount
		// dimension. Either way there is nothing to group on, so skip.
		if dimension == nil {
			continue
		}
		grouped[dimension.Amount.Cents] = append(grouped[dimension.Amount.Cents], offer)
	}
	return grouped
}

// OfferPremium returns the premium facade for an offer: the V3Premium of the
// single primary (best-qualifying) pricing row, or nil when the offer has no
// qualifying row (every row ineligible, or the rare eligible row whose carrier
// returned no priceable mode). This is the one premium a list UI shows per
// product without walking Pricing.
func OfferPremium(offer V3Offer) *V3Premium {
	for i := range offer.Pricing {
		if offer.Pricing[i].Primary {
			return offer.Pricing[i].Premium
		}
	}
	return nil
}

// PrequalifyV3Options carries request controls unique to the v3 API.
type PrequalifyV3Options struct {
	OnlyProductClass            string   `json:"only_product_class,omitempty"`
	IncludeProductClass         []string `json:"include_product_class,omitempty"`
	MinRank                     string   `json:"min_rank,omitempty"`
	ShowUnreleased              *bool    `json:"show_unreleased,omitempty"`
	SkipHealthBasedUnderwriting *bool    `json:"skip_health_based_underwriting,omitempty"`
	IncludeIneligible           *bool    `json:"include_ineligible,omitempty"`
}

// PrequalifyV3Request is the typed request shape for POST /v3/prequalify.
type PrequalifyV3Request struct {
	Applicant Applicant            `json:"applicant"`
	Coverage  Coverage             `json:"coverage"`
	Products  ProductSelection     `json:"products"`
	Options   *PrequalifyV3Options `json:"options,omitempty"`
}

// QuoteV3Options carries request controls unique to the v3 quote API.
type QuoteV3Options = PrequalifyV3Options

// QuoteV3Request is the typed request shape for POST /v3/quote.
type QuoteV3Request struct {
	Applicant Applicant        `json:"applicant"`
	Coverage  Coverage         `json:"coverage"`
	Products  ProductSelection `json:"products"`
	Options   *QuoteV3Options  `json:"options,omitempty"`
}

// PrequalifyV3Service implements POST /v3/prequalify. Construct via
// NewClient; the public surface is Run.
type PrequalifyV3Service struct {
	client *Client
}

// QuoteV3Service implements POST /v3/quote. Construct via NewClient.
type QuoteV3Service struct {
	client *Client
}
