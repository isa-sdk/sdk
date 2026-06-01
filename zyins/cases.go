// Package zyins — cases + email sub-services.
//
// Cases (POST /v1/case) are content-addressed shareable artifacts created
// from a quote input + results + selected products. The server hashes
// the (xml, results, products) tuple — identical inputs dedupe to the
// same hash regardless of which license created the case.
//
// Email (POST /v1/email/enqueue) is the transactional email surface
// the WordPress plugin and bpp2.0 use today. CasesService.Email and
// EmailService.Enqueue both target the same wire endpoint; both exist
// so callers can pick whichever namespace matches their mental model.
//
// Future ListCases / GetCase / DeleteCase / GetMessage / ListMessages
// operations require net-new server work tracked in the design doc.

package zyins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	caseCreatePath   = "/v1/case"
	emailEnqueuePath = "/v1/email/enqueue"
)

// CasesService groups case create + case-share email.
type CasesService struct {
	client *Client
}

// EmailService groups transactional email operations.
type EmailService struct {
	client *Client
}

// CaseCreateInput is the input shape for CasesService.Create.
//
// Input is polymorphic at the wire: a struct value (or map) is converted
// to XML server-side; a string is treated as raw XML. The SDK encodes
// whatever Go value is supplied as JSON, matching the server's
// polymorphic acceptance.
type CaseCreateInput struct {
	// Input is the quote input — a structured value or raw XML string.
	// Required.
	Input any
	// Results is an optional quote-results value (any JSON-serializable).
	Results any
	// Products is an optional list of product identifiers selected by
	// the agent.
	Products []string
}

// CaseCreateResult is the response from CasesService.Create.
type CaseCreateResult struct {
	Object    string `json:"object"`
	Hash      string `json:"hash"`
	URL       string `json:"url"`
	Readonly  bool   `json:"readonly"`
	CreatedAt string `json:"created_at"`
}

// caseCreateWireBody mirrors the on-wire JSON shape.
type caseCreateWireBody struct {
	Input    any      `json:"input"`
	Results  any      `json:"results,omitempty"`
	Products []string `json:"products,omitempty"`
}

// Share creates a new shareable case. Canonical verb per the locked
// SDK syntax (TS canon: isa.zyins.cases.share). Create is retained
// as a deprecated alias.
func (s *CasesService) Share(ctx context.Context, input *CaseCreateInput, opts ...RunOption) (*CaseCreateResult, error) {
	if input == nil {
		return nil, validationFailure("zyins: CaseCreateInput is nil")
	}
	if input.Input == nil {
		return nil, validationFailure("zyins: CaseCreateInput.Input is required")
	}
	if s, ok := input.Input.(string); ok && strings.TrimSpace(s) == "" {
		return nil, validationFailure("zyins: CaseCreateInput.Input must be non-empty")
	}
	ro := collectRunOptions(opts)
	raw, err := s.client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           caseCreatePath,
		body:           caseCreateWireBody{Input: input.Input, Results: input.Results, Products: input.Products},
		op:             "cases_share",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Cases.Share: %w", err)
	}
	return decodeCaseCreateResponse(raw, "cases_share")
}

// Create creates a new shareable case.
//
// Deprecated: use Share.
func (s *CasesService) Create(ctx context.Context, input *CaseCreateInput, opts ...RunOption) (*CaseCreateResult, error) {
	return s.Share(ctx, input, opts...)
}

// Email is the case-share convenience for emailing a case PDF. It
// targets POST /v1/email/enqueue (same as EmailService.Enqueue).
func (s *CasesService) Email(ctx context.Context, input *EmailEnqueueInput, opts ...RunOption) (*EmailEnqueueResult, error) {
	return enqueueEmail(ctx, s.client, input, opts...)
}

// EmailEnqueueInput is the input shape for EmailService.Enqueue and
// CasesService.Email.
type EmailEnqueueInput struct {
	To                 string
	Subject            string
	BodyHTML           string
	AttachmentFilename string
	// AttachmentContent is the raw attachment bytes; the SDK
	// base64-encodes them before sending.
	AttachmentContent []byte
}

// EmailEnqueueResult is the response from email enqueue.
type EmailEnqueueResult struct {
	EnqueueID string `json:"enqueue_id"`
}

type emailWireAttachment struct {
	Filename      string `json:"filename"`
	ContentBase64 string `json:"content_base64"`
}

type emailWireBody struct {
	To         string               `json:"to"`
	Subject    string               `json:"subject"`
	BodyHTML   string               `json:"body_html"`
	Attachment *emailWireAttachment `json:"attachment,omitempty"`
}

// Enqueue posts a transactional email payload to /v1/email/enqueue.
func (s *EmailService) Enqueue(ctx context.Context, input *EmailEnqueueInput, opts ...RunOption) (*EmailEnqueueResult, error) {
	return enqueueEmail(ctx, s.client, input, opts...)
}

// enqueueEmail sends the shared email payload used by both case and
// email namespaces.
func enqueueEmail(ctx context.Context, client *Client, input *EmailEnqueueInput, opts ...RunOption) (*EmailEnqueueResult, error) {
	if input == nil {
		return nil, validationFailure("zyins: EmailEnqueueInput is nil")
	}
	if strings.TrimSpace(input.To) == "" {
		return nil, validationFailure("zyins: EmailEnqueueInput.To is required")
	}
	body := emailWireBody{To: input.To, Subject: input.Subject, BodyHTML: input.BodyHTML}
	if input.AttachmentFilename != "" || len(input.AttachmentContent) > 0 {
		body.Attachment = &emailWireAttachment{
			Filename:      input.AttachmentFilename,
			ContentBase64: base64.StdEncoding.EncodeToString(input.AttachmentContent),
		}
	}
	ro := collectRunOptions(opts)
	raw, err := client.doJSON(ctx, requestArgs{
		method:         http.MethodPost,
		path:           emailEnqueuePath,
		body:           body,
		op:             "email_enqueue",
		idempotencyKey: ro.idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("zyins: Email.Enqueue: %w", err)
	}
	return decodeEmailEnqueueResponse(raw)
}

// decodeCaseCreateResponse decodes case-share responses and uses op
// only to report the caller-facing operation in errors.
func decodeCaseCreateResponse(body []byte, op string) (*CaseCreateResult, error) {
	data, err := unwrapEnvelope(body, op)
	if err != nil {
		return nil, err
	}
	var result CaseCreateResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode %s response: %w", op, err)
	}
	if result.Object == "" {
		result.Object = "case"
	}
	return &result, nil
}

// decodeEmailEnqueueResponse decodes email enqueue responses, including
// empty success bodies from legacy handlers.
func decodeEmailEnqueueResponse(body []byte) (*EmailEnqueueResult, error) {
	if len(body) == 0 {
		return &EmailEnqueueResult{}, nil
	}
	data, err := unwrapEnvelope(body, "email_enqueue")
	if err != nil {
		return nil, err
	}
	var result EmailEnqueueResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("zyins: failed to decode email_enqueue response: %w", err)
	}
	return &result, nil
}
