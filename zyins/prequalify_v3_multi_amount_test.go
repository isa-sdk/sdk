package zyins

// Multi-amount PrequalifyV3.Run — native coverage.quote_options request +
// flat `plans[]` response with the v3 Money primitive (zyins #400, Money
// cutover).
//
// Every v3 request — single and multi-amount alike — answers with one
// flat `plans[]` array. A single face amount keeps the proven
// {face_amount_cents} coverage; a multi-amount probe sends
// coverage.quote_options (mirroring /v3/quote). Group client-side with
// ByAmount on the requested dimension.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// multiAmountV3Server returns a flat `plans[]` envelope so the decoder is
// exercised end-to-end.
func multiAmountV3Server(t *testing.T, responseBody string) *capturingV3Server {
	t.Helper()
	c := &capturingV3Server{}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.path = r.URL.Path
		c.method = r.Method
		c.header = r.Header.Clone()
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		c.body = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(c.server.Close)
	return c
}

const flatFaceV3Response = `{
	"object":"prequalify_result",
	"request_id":"req_01HZK2N5GQR9T8X4B6FJW3Y1AS",
	"idempotency_key":"550e8400-e29b-41d4-a716-446655440000",
	"livemode":true,
	"data":{"plans":[
		{"object":"plan_offer","id":"p1","eligible":true,"plan_info":[],"metadata":{},"death_benefit":{"amount":{"cents":2500000,"display":"$25,000"},"period":null},"pricing":[{"rate_class":"Preferred","primary":true,"eligibility":{"category":"immediate","eligible":true,"reasons":[]},"premium":{"amount":{"cents":4500,"display":"$45.00"},"default_mode":"MONTHLY-EFT","modes":{"MONTHLY-EFT":{"cents":4500,"display":"$45.00"}}},"rank":1}]},
		{"object":"plan_offer","id":"p2","eligible":true,"plan_info":[],"metadata":{},"death_benefit":{"amount":{"cents":5000000,"display":"$50,000"},"period":null},"pricing":[{"rate_class":"Preferred","primary":true,"eligibility":{"category":"immediate","eligible":true,"reasons":[]},"premium":{"amount":{"cents":8100,"display":"$81.00"},"default_mode":"MONTHLY-EFT","modes":{"MONTHLY-EFT":{"cents":8100,"display":"$81.00"}}},"rank":1}]}
	]}
}`

const flatBudgetV3Response = `{
	"object":"prequalify_result",
	"request_id":"r","idempotency_key":"k","livemode":true,
	"data":{"plans":[
		{"object":"plan_offer","id":"b1","eligible":true,"plan_info":[],"metadata":{},"death_benefit":{"amount":{"cents":5000000,"display":"$50,000"},"period":null},"budget":{"amount":{"cents":5000,"display":"$50.00"},"period":"monthly"},"pricing":[]},
		{"object":"plan_offer","id":"b2","eligible":true,"plan_info":[],"metadata":{},"death_benefit":{"amount":{"cents":7500000,"display":"$75,000"},"period":null},"budget":{"amount":{"cents":7500,"display":"$75.00"},"period":"monthly"},"pricing":[]}
	]}
}`

func multiV3Request(t *testing.T) *PrequalifyV3Request {
	t.Helper()
	cov, err := NewFaceValuesCoverage([]int{25_000, 50_000})
	if err != nil {
		t.Fatalf("NewFaceValuesCoverage: %v", err)
	}
	products, err := NewProductSelection("fidelity-life-instabrain-pure-term")
	if err != nil {
		t.Fatalf("NewProductSelection: %v", err)
	}
	return &PrequalifyV3Request{
		Applicant: validApplicant(t),
		Coverage:  cov,
		Products:  products,
	}
}

func TestPrequalifyV3Run_MultiAmount_EmitsQuoteOptions(t *testing.T) {
	srv := multiAmountV3Server(t, flatFaceV3Response)
	client := newV3PinnedClient(t, srv.server)

	if _, err := client.PrequalifyV3.Run(context.Background(), multiV3Request(t)); err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}

	var body struct {
		Coverage struct {
			FaceAmountCents *int `json:"face_amount_cents"`
			State           string
			QuoteOptions    struct {
				QuoteType string   `json:"quote_type"`
				Amounts   []string `json:"amounts"`
			} `json:"quote_options"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal(srv.body, &body); err != nil {
		t.Fatalf("decode request body: %v\nbody=%s", err, string(srv.body))
	}
	if body.Coverage.FaceAmountCents != nil {
		t.Errorf("multi-amount coverage must not carry face_amount_cents: %d", *body.Coverage.FaceAmountCents)
	}
	if got, want := body.Coverage.QuoteOptions.QuoteType, "face_amounts"; got != want {
		t.Errorf("coverage.quote_options.quote_type = %q, want %q", got, want)
	}
	if got := body.Coverage.QuoteOptions.Amounts; len(got) != 2 || got[0] != "25000" || got[1] != "50000" {
		t.Errorf("coverage.quote_options.amounts = %v, want [25000 50000]", got)
	}
}

func TestPrequalifyV3Run_MultiAmount_ParsesFlatPlansAsMoney(t *testing.T) {
	srv := multiAmountV3Server(t, flatFaceV3Response)
	client := newV3PinnedClient(t, srv.server)

	out, err := client.PrequalifyV3.Run(context.Background(), multiV3Request(t))
	if err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}
	if len(out.Plans) != 2 {
		t.Fatalf("Plans = %d, want 2", len(out.Plans))
	}
	if out.Plans[0].DeathBenefit == nil {
		t.Fatal("Plans[0].DeathBenefit must be non-nil for a life product")
	}
	if got := out.Plans[0].DeathBenefit.Amount.Cents; got != 2500000 {
		t.Errorf("Plans[0].DeathBenefit.Amount.Cents = %d, want 2500000", got)
	}
	if out.Plans[0].DeathBenefit.Period != nil {
		t.Errorf("face-amount death benefit period must be nil, got %v", *out.Plans[0].DeathBenefit.Period)
	}
	if out.Plans[0].Budget != nil {
		t.Errorf("face-amount offer must not carry budget, got %+v", out.Plans[0].Budget)
	}
	if out.Plans[1].Pricing[0].Premium == nil || out.Plans[1].Pricing[0].Premium.Amount.Cents != 8100 {
		t.Errorf("Plans[1] premium not decoded: %+v", out.Plans[1].Pricing)
	}
}

func TestPrequalifyV3Run_MultiAmount_ByAmountGroupsByDeathBenefit(t *testing.T) {
	srv := multiAmountV3Server(t, flatFaceV3Response)
	client := newV3PinnedClient(t, srv.server)

	out, err := client.PrequalifyV3.Run(context.Background(), multiV3Request(t))
	if err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}
	grouped := ByAmount(out.Plans)
	if len(grouped) != 2 {
		t.Fatalf("ByAmount produced %d groups, want 2", len(grouped))
	}
	if len(grouped[2500000]) != 1 || len(grouped[5000000]) != 1 {
		t.Errorf("ByAmount groups wrong: %+v", grouped)
	}
}

func TestPrequalifyV3Run_MonthlyBudget_DecodesBudgetAndGroupsByBudget(t *testing.T) {
	srv := multiAmountV3Server(t, flatBudgetV3Response)
	client := newV3PinnedClient(t, srv.server)

	cov, err := NewMonthlyBudgetsCoverage([]int{50, 75})
	if err != nil {
		t.Fatalf("NewMonthlyBudgetsCoverage: %v", err)
	}
	products, err := NewProductSelection("fidelity-life-instabrain-pure-term")
	if err != nil {
		t.Fatalf("NewProductSelection: %v", err)
	}
	out, err := client.PrequalifyV3.Run(context.Background(), &PrequalifyV3Request{
		Applicant: validApplicant(t),
		Coverage:  cov,
		Products:  products,
	})
	if err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}
	if out.Plans[0].Budget == nil {
		t.Fatalf("monthly-budget offer must carry budget")
	}
	if got := out.Plans[0].Budget.Amount.Cents; got != 5000 {
		t.Errorf("Budget.Amount.Cents = %d, want 5000", got)
	}
	if out.Plans[0].Budget.Period == nil || *out.Plans[0].Budget.Period != V3PeriodMonthly {
		t.Errorf("Budget.Period must be monthly")
	}
	grouped := ByAmount(out.Plans)
	if len(grouped[5000]) != 1 || len(grouped[7500]) != 1 {
		t.Errorf("ByAmount must group budget responses by budget cents: %+v", grouped)
	}
}

func TestByAmount_BudgetMode_SkipsOfferMissingBudget(t *testing.T) {
	monthly := V3PeriodMonthly
	plans := []V3Offer{
		{
			ID:           "offer_with_budget",
			Budget:       &V3Money{Amount: V3Amount{Cents: 5000}, Period: &monthly},
			DeathBenefit: &V3Money{Amount: V3Amount{Cents: 2500000}},
		},
		{
			// In budget mode this offer is missing budget; it must be
			// skipped rather than mis-bucketed under its death benefit.
			ID:           "offer_missing_budget",
			DeathBenefit: &V3Money{Amount: V3Amount{Cents: 5000000}},
		},
	}

	grouped := ByAmount(plans)
	if len(grouped) != 1 {
		t.Fatalf("ByAmount produced %d groups, want 1 (missing-budget skipped): %+v", len(grouped), grouped)
	}
	if len(grouped[5000]) != 1 {
		t.Errorf("budget bucket 5000 = %d offers, want 1", len(grouped[5000]))
	}
	if _, mis := grouped[5000000]; mis {
		t.Errorf("missing-budget offer mis-bucketed under death_benefit cents 5000000: %+v", grouped)
	}
}

func TestPrequalifyV3Run_FlatEmptyPlans(t *testing.T) {
	const flat = `{"object":"prequalify_result","request_id":"r","idempotency_key":"k","livemode":true,"data":{"plans":[]}}`
	srv := multiAmountV3Server(t, flat)
	client := newV3PinnedClient(t, srv.server)

	out, err := client.PrequalifyV3.Run(context.Background(), multiV3Request(t))
	if err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}
	if len(out.Plans) != 0 {
		t.Errorf("empty response Plans = %d, want 0", len(out.Plans))
	}
}
