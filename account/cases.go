package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const casesPath = "/v1/case"

// CaseCreateInput is the request shape for Cases.Create. Input is the
// caller's quote payload (object → JSON; string → forwarded verbatim).
type CaseCreateInput struct {
	// Input is the quote input. Object is JSON-marshaled; string passes
	// through (caller has already serialized to XML or JSON).
	Input any
	// Results is the optional engine output payload.
	Results any
	// Products is the optional product selection.
	Products []string
}

// CaseCreateResult is the response shape for Cases.Create.
type CaseCreateResult struct {
	Hash      string `json:"hash"`
	URL       string `json:"url"`
	Readonly  bool   `json:"readonly"`
	CreatedAt string `json:"created_at"`
}

// CaseSummary is the get/list response shape. Input / Results / Products
// are present only when the server returns them (caller owns the case).
type CaseSummary struct {
	Hash      string   `json:"hash"`
	URL       string   `json:"url"`
	Readonly  bool     `json:"readonly"`
	CreatedAt string   `json:"created_at"`
	Input     any      `json:"input,omitempty"`
	Results   any      `json:"results,omitempty"`
	Products  []string `json:"products,omitempty"`
}

// CaseEmailInput is the request shape for Cases.Email.
type CaseEmailInput struct {
	CaseID string
	To     string
}

// CasesService is the `account.cases` facade.
type CasesService struct {
	client *Client
}

// Create posts a new shareable case.
func (s *CasesService) Create(ctx context.Context, in CaseCreateInput, opts ...CallOption) (*CaseCreateResult, error) {
	if in.Input == nil {
		return nil, errors.New("account: Cases.Create requires Input")
	}
	wire := map[string]any{"input": in.Input}
	if in.Results != nil {
		wire["results"] = in.Results
	}
	if len(in.Products) > 0 {
		wire["products"] = in.Products
	}
	bodyBytes, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Create marshal: %w", err)
	}
	co := collectCallOptions(opts)
	body, err := s.client.signedDo(ctx, callArgs{
		method:         http.MethodPost,
		path:           casesPath,
		body:           bodyBytes,
		idempotencyKey: co.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Create: %w", err)
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Create envelope: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("account: Cases.Create response body was empty")
	}
	out := &CaseCreateResult{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("account: Cases.Create decode: %w", err)
	}
	return out, nil
}

// Get retrieves a single case by its content-addressed hash.
func (s *CasesService) Get(ctx context.Context, caseID string) (*CaseSummary, error) {
	if caseID == "" {
		return nil, errors.New("account: Cases.Get requires a non-empty caseID")
	}
	path := casesPath + "/" + url.PathEscape(caseID)
	body, err := s.client.signedDo(ctx, callArgs{method: http.MethodGet, path: path})
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Get: %w", err)
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, fmt.Errorf("account: Cases.Get envelope: %w", err)
	}
	out := &CaseSummary{}
	if len(data) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("account: Cases.Get decode: %w", err)
	}
	return out, nil
}

// List returns every case visible to the caller.
func (s *CasesService) List(ctx context.Context) ([]CaseSummary, error) {
	body, err := s.client.signedDo(ctx, callArgs{method: http.MethodGet, path: casesPath})
	if err != nil {
		return nil, fmt.Errorf("account: Cases.List: %w", err)
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, fmt.Errorf("account: Cases.List envelope: %w", err)
	}
	if len(data) == 0 {
		return []CaseSummary{}, nil
	}
	// Try bare array first.
	var asArray []CaseSummary
	if err := json.Unmarshal(data, &asArray); err == nil {
		return asArray, nil
	}
	// Try `{cases: [...]}` shape.
	var withCases struct {
		Cases []CaseSummary `json:"cases"`
	}
	if err := json.Unmarshal(data, &withCases); err != nil {
		return nil, fmt.Errorf("account: Cases.List decode: %w", err)
	}
	if withCases.Cases == nil {
		return []CaseSummary{}, nil
	}
	return withCases.Cases, nil
}

// Email enqueues a transactional send of the case PDF / artifact.
func (s *CasesService) Email(ctx context.Context, in CaseEmailInput, opts ...CallOption) (bool, error) {
	if in.CaseID == "" {
		return false, errors.New("account: Cases.Email requires a non-empty CaseID")
	}
	if in.To == "" {
		return false, errors.New("account: Cases.Email requires a non-empty To")
	}
	path := casesPath + "/" + url.PathEscape(in.CaseID) + "/email"
	bodyBytes, err := json.Marshal(map[string]string{"to": in.To})
	if err != nil {
		return false, fmt.Errorf("account: Cases.Email marshal: %w", err)
	}
	co := collectCallOptions(opts)
	if _, err := s.client.signedDo(ctx, callArgs{
		method:         http.MethodPost,
		path:           path,
		body:           bodyBytes,
		idempotencyKey: co.idempotencyKey,
	}); err != nil {
		return false, fmt.Errorf("account: Cases.Email: %w", err)
	}
	return true, nil
}
