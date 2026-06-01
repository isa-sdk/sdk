package zyins

// Wire-shape contract tests for PrequalifyV3.Run.
//
// Prod incident 2026-05-29: the v3 prequalify marshaler emitted the v2
// flat shape (`date_of_birth`, `gender`, `height`, `weight` at the
// root) against POST /v3/prequalify, which rejects unknown fields and
// requires the envelope shape from PrequalifyV3Request
// (`applicant` + `coverage` + `products[]`).
//
// These tests pin the wire body to the OpenAPI source-of-truth schemas
// in go/zyins/api/openapi.yaml so the bug cannot regress silently:
//   - PrequalifyV3.Run MUST emit the v3 envelope, POST to
//     /v3/prequalify, and carry `Api-Version: v3`.
//   - QuoteV3.Run MUST continue to emit the legacy v3 flat shape and
//     POST to /v3/quote — preserved untouched by the v3 fix.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturingV3Server stands in for the zyins server. It records the
// request URL, method, headers, and body, then returns a minimal but
// well-formed v3 envelope so the SDK decoder is exercised end-to-end.
type capturingV3Server struct {
	server *httptest.Server
	path   string
	method string
	header http.Header
	body   []byte
}

func newCapturingV3Server(t *testing.T) *capturingV3Server {
	t.Helper()
	c := &capturingV3Server{}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.path = r.URL.Path
		c.method = r.Method
		c.header = r.Header.Clone()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("server: read request body: %v", err)
		}
		c.body = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object":"prequalify_result",
			"request_id":"req_01HZK2N5GQR9T8X4B6FJW3Y1AS",
			"idempotency_key":"550e8400-e29b-41d4-a716-446655440000",
			"livemode":true,
			"data":{"plans":[]}
		}`))
	}))
	t.Cleanup(c.server.Close)
	return c
}

// v3PrequalifyEnvelope is the strongly typed contract this fix
// guarantees. A struct match (rather than a free-form map) prevents
// silent reintroduction of v2 keys at the envelope root.
type v3PrequalifyEnvelope struct {
	Applicant         v3ApplicantInput `json:"applicant"`
	Coverage          v3CoverageInput  `json:"coverage"`
	Products          []string         `json:"products"`
	IncludeIneligible bool             `json:"include_ineligible"`

	// Forbidden v2 root fields. json.Decoder with DisallowUnknownFields
	// is asserted separately to fail loudly on any other regression.
	DateOfBirth   *string         `json:"date_of_birth,omitempty"`
	Gender        *string         `json:"gender,omitempty"`
	Height        *int            `json:"height,omitempty"`
	Weight        *int            `json:"weight,omitempty"`
	State         *string         `json:"state,omitempty"`
	NicotineUsage json.RawMessage `json:"nicotine_usage,omitempty"`
	QuoteOptions  json.RawMessage `json:"quote_options,omitempty"`
}

type v3ApplicantInput struct {
	Sex          string             `json:"sex"`
	DOB          string             `json:"dob"`
	HeightInches int                `json:"height_inches"`
	WeightLbs    int                `json:"weight_lbs"`
	Nicotine     v3NicotineInput    `json:"nicotine"`
	Conditions   []v3ConditionInput `json:"conditions,omitempty"`
	Medications  []v3MedicationInpt `json:"medications,omitempty"`
}

type v3CoverageInput struct {
	FaceAmountCents int     `json:"face_amount_cents"`
	State           string  `json:"state"`
	Zip             *string `json:"zip,omitempty"`
}

type v3NicotineInput struct {
	LastUsed    string              `json:"last_used"`
	Specificity []v3NicotineSpecRow `json:"specificity,omitempty"`
}

type v3NicotineSpecRow struct {
	Text      string `json:"text"`
	Frequency string `json:"frequency"`
}

type v3ConditionInput struct {
	Text          string `json:"text"`
	WasDiagnosed  string `json:"was_diagnosed,omitempty"`
	LastTreatment string `json:"last_treatment,omitempty"`
}

type v3MedicationInpt struct {
	Text      string `json:"text"`
	Use       string `json:"use,omitempty"`
	FirstFill string `json:"first_fill,omitempty"`
	LastFill  string `json:"last_fill,omitempty"`
}

// v3TestApplicant returns an applicant with conditions, medications,
// and nicotine specificity populated so every field of the envelope is
// covered by one test request.
func v3TestApplicant(t *testing.T) Applicant {
	t.Helper()
	a := validApplicant(t)
	a.Conditions = []Condition{
		{Name: "High Blood Pressure", WasDiagnosed: "5 YEARS AGO", LastTreatment: "2 MONTHS AGO"},
	}
	a.Medications = []Medication{
		{Name: "Lisinopril", Use: "High Blood Pressure", FirstFill: "5 YEARS AGO", LastFill: "1 MONTH AGO"},
	}
	a.NicotineUse = NicotineUsageInput{
		LastUsed:     NicotineWithin12Months,
		ProductUsage: []NicotineProductUsage{{Type: "CIGARETTE", Frequency: "DAILY"}},
	}
	return a
}

func v3TestRequest(t *testing.T) *PrequalifyV3Request {
	t.Helper()
	cov, err := NewFaceValueCoverage(100_000)
	if err != nil {
		t.Fatalf("NewFaceValueCoverage: %v", err)
	}
	products, err := NewProductSelection("fidelity-life-instabrain-pure-term")
	if err != nil {
		t.Fatalf("NewProductSelection: %v", err)
	}
	return &PrequalifyV3Request{
		Applicant: v3TestApplicant(t),
		Coverage:  cov,
		Products:  products,
	}
}

func newV3PinnedClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(
		WithToken("isa_test_4fjK2nQ7mX1aB8sR9pZ3"),
		WithBaseURL(srv.URL),
		WithMaxRetryAttempts(1),
		WithAPIVersionOverrides(map[string]string{
			surfacePrequalify: apiVersionV3,
			surfaceQuote:      apiVersionV3,
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestPrequalifyV3Run_EmitsEnvelopeShape(t *testing.T) {
	srv := newCapturingV3Server(t)
	client := newV3PinnedClient(t, srv.server)

	_, err := client.PrequalifyV3.Run(context.Background(), v3TestRequest(t))
	if err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}

	if got, want := srv.method, http.MethodPost; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := srv.path, prequalifyV3Path; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := srv.header.Get(apiVersionHeader), apiVersionV3; got != want {
		t.Errorf("%s header = %q, want %q", apiVersionHeader, got, want)
	}

	var env v3PrequalifyEnvelope
	dec := json.NewDecoder(bytes.NewReader(srv.body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("decode v3 envelope (request body must match the typed contract): %v\nbody=%s", err, string(srv.body))
	}

	// Envelope must NOT carry any v2 flat root keys.
	if env.DateOfBirth != nil || env.Gender != nil || env.Height != nil || env.Weight != nil || env.State != nil {
		t.Errorf("v2 flat root fields leaked into v3 envelope: dob=%v gender=%v height=%v weight=%v state=%v",
			env.DateOfBirth, env.Gender, env.Height, env.Weight, env.State)
	}
	if len(env.NicotineUsage) > 0 || len(env.QuoteOptions) > 0 {
		t.Errorf("v2 nicotine_usage / quote_options leaked into v3 envelope: nic=%s qo=%s",
			string(env.NicotineUsage), string(env.QuoteOptions))
	}

	if got, want := env.Applicant.Sex, "male"; got != want {
		t.Errorf("applicant.sex = %q, want %q", got, want)
	}
	if got, want := env.Applicant.DOB, "1962-04-18"; got != want {
		t.Errorf("applicant.dob = %q, want %q", got, want)
	}
	if got, want := env.Applicant.HeightInches, 70; got != want {
		t.Errorf("applicant.height_inches = %d, want %d", got, want)
	}
	if got, want := env.Applicant.WeightLbs, 195; got != want {
		t.Errorf("applicant.weight_lbs = %d, want %d", got, want)
	}
	if got, want := env.Applicant.Nicotine.LastUsed, string(NicotineWithin12Months); got != want {
		t.Errorf("applicant.nicotine.last_used = %q, want %q", got, want)
	}
	if len(env.Applicant.Nicotine.Specificity) != 1 ||
		env.Applicant.Nicotine.Specificity[0].Text != "CIGARETTE" ||
		env.Applicant.Nicotine.Specificity[0].Frequency != "daily" {
		t.Errorf("applicant.nicotine.specificity = %+v, want [{CIGARETTE daily}] (DAILY must map to v3 enum 'daily')",
			env.Applicant.Nicotine.Specificity)
	}
	if len(env.Applicant.Conditions) != 1 || env.Applicant.Conditions[0].Text != "High Blood Pressure" {
		t.Errorf("applicant.conditions = %+v, want one row with text='High Blood Pressure'", env.Applicant.Conditions)
	}
	if len(env.Applicant.Medications) != 1 || env.Applicant.Medications[0].Text != "Lisinopril" {
		t.Errorf("applicant.medications = %+v, want one row with text='Lisinopril'", env.Applicant.Medications)
	}

	// Coverage envelope: face_amount_cents (dollars * 100) + state.
	if got, want := env.Coverage.FaceAmountCents, 100_000*centsPerDollar; got != want {
		t.Errorf("coverage.face_amount_cents = %d, want %d", got, want)
	}
	if got, want := env.Coverage.State, "NC"; got != want {
		t.Errorf("coverage.state = %q, want %q", got, want)
	}
	// An applicant with no zip must NOT emit coverage.zip — the server
	// pattern ^\d{5}(-\d{4})?$ rejects an empty string for non-medsup
	// callers who left it blank.
	if env.Coverage.Zip != nil {
		t.Errorf("coverage.zip = %q, want absent (no applicant zip supplied)", *env.Coverage.Zip)
	}

	// Products list: flat slugs, in caller-preferred order.
	if len(env.Products) != 1 || env.Products[0] != "fidelity-life-instabrain-pure-term" {
		t.Errorf("products = %v, want [fidelity-life-instabrain-pure-term]", env.Products)
	}

	if !env.IncludeIneligible {
		t.Errorf("include_ineligible = false, want true (default)")
	}
}

// TestPrequalifyV3Run_ThreadsApplicantZipIntoCoverage pins the medsup
// zip fix: applicant.Zip MUST surface as coverage.zip on the v3
// prequalify envelope. Before the fix the serializer dropped zip, so the
// server zip-gated and silently filtered medsup products (prod incident,
// bpp2.0). This test fails on the dropped-zip behavior and passes once
// zip rides the coverage envelope, mirroring the /v3/quote path.
func TestPrequalifyV3Run_ThreadsApplicantZipIntoCoverage(t *testing.T) {
	srv := newCapturingV3Server(t)
	client := newV3PinnedClient(t, srv.server)

	req := v3TestRequest(t)
	req.Applicant.Zip = "75001"

	if _, err := client.PrequalifyV3.Run(context.Background(), req); err != nil {
		t.Fatalf("PrequalifyV3.Run: %v", err)
	}

	var env v3PrequalifyEnvelope
	dec := json.NewDecoder(bytes.NewReader(srv.body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("decode v3 envelope: %v\nbody=%s", err, string(srv.body))
	}
	if env.Coverage.Zip == nil {
		t.Fatalf("coverage.zip absent, want %q (applicant zip must thread into coverage); body=%s", "75001", string(srv.body))
	}
	if got, want := *env.Coverage.Zip, "75001"; got != want {
		t.Errorf("coverage.zip = %q, want %q", got, want)
	}
}

func TestQuoteV3Run_PreservesLegacyFlatShape(t *testing.T) {
	srv := newCapturingV3Server(t)
	srv.server.Close()
	// Replace the canned envelope with the quote-shaped data block.
	quoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.path = r.URL.Path
		srv.method = r.Method
		srv.header = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		srv.body = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object":"quote_result",
			"request_id":"req_01HZK2N5GQR9T8X4B6FJW3Y1AS",
			"idempotency_key":"550e8400-e29b-41d4-a716-446655440000",
			"livemode":true,
			"data":{"plans":[]}
		}`))
	}))
	t.Cleanup(quoteSrv.Close)
	client := newV3PinnedClient(t, quoteSrv)

	cov, err := NewFaceValueCoverage(100_000)
	if err != nil {
		t.Fatalf("NewFaceValueCoverage: %v", err)
	}
	products, err := NewProductSelection("fidelity-life-instabrain-pure-term")
	if err != nil {
		t.Fatalf("NewProductSelection: %v", err)
	}
	req := &QuoteV3Request{
		Applicant: v3TestApplicant(t),
		Coverage:  cov,
		Products:  products,
	}
	if _, err := client.QuoteV3.Run(context.Background(), req); err != nil {
		t.Fatalf("QuoteV3.Run: %v", err)
	}

	if got, want := srv.path, quoteV3Path; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	// QuoteV3 deliberately keeps the legacy flat shape; the body MUST
	// carry the v2 root keys until /v3/quote migrates to its own
	// envelope.
	var flat map[string]any
	if err := json.Unmarshal(srv.body, &flat); err != nil {
		t.Fatalf("decode flat body: %v\nbody=%s", err, string(srv.body))
	}
	for _, k := range []string{"date_of_birth", "gender", "height", "weight", "state", "nicotine_usage", "quote_options"} {
		if _, ok := flat[k]; !ok {
			t.Errorf("quote_v3 body missing legacy root key %q (regression — quote MUST keep the flat shape until its own envelope ships); body=%s", k, string(srv.body))
		}
	}
	if _, ok := flat["applicant"]; ok {
		t.Errorf("quote_v3 body unexpectedly carries v3 envelope key 'applicant'; body=%s", string(srv.body))
	}
}

func TestPrequalifyV3Run_WithSingleMonthlyBudget_SerializesQuoteOptions(t *testing.T) {
	srv := newCapturingV3Server(t)
	client := newV3PinnedClient(t, srv.server)

	cov, err := NewMonthlyBudgetCoverage(50)
	if err != nil {
		t.Fatalf("NewMonthlyBudgetCoverage: %v", err)
	}
	products, err := NewProductSelection("fidelity-life-instabrain-pure-term")
	if err != nil {
		t.Fatalf("NewProductSelection: %v", err)
	}
	req := &PrequalifyV3Request{
		Applicant: v3TestApplicant(t),
		Coverage:  cov,
		Products:  products,
	}

	// A single monthly budget rides the quote_options block with one
	// amount and the monthly_budget discriminator — it must NOT throw, and
	// must NOT serialize as a face amount (a $50/month budget is not a $50
	// face amount).
	if _, err = client.PrequalifyV3.Run(context.Background(), req); err != nil {
		t.Fatalf("PrequalifyV3.Run with single monthly-budget: got error %v, want success", err)
	}
	var body struct {
		Coverage struct {
			FaceAmountCents *int `json:"face_amount_cents"`
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
		t.Errorf("monthly-budget coverage must not carry face_amount_cents: %d", *body.Coverage.FaceAmountCents)
	}
	if got, want := body.Coverage.QuoteOptions.QuoteType, "monthly_budget"; got != want {
		t.Errorf("coverage.quote_options.quote_type = %q, want %q", got, want)
	}
	if got := body.Coverage.QuoteOptions.Amounts; len(got) != 1 || got[0] != "50" {
		t.Errorf("coverage.quote_options.amounts = %v, want [50]", got)
	}
}
