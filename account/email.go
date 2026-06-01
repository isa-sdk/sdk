package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const emailEnqueuePath = "/v1/email/enqueue"

// EmailAttachment is one attachment in an email-enqueue request. Content
// is the base64-encoded payload; the SDK passes through verbatim so
// binary attachments (PDFs) do not pay a UTF-8 round-trip.
type EmailAttachment struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// EmailEnqueueInput is the request shape for Email.Enqueue. Either a
// single address or a slice may be supplied via To; an empty slice is
// rejected as a programming error.
type EmailEnqueueInput struct {
	To          []string
	Subject     string
	Body        string
	Attachments []EmailAttachment
}

// EmailService is the `account.email` facade.
type EmailService struct {
	client *Client
}

// Enqueue posts a transactional email request to the server. The
// server response is normalized — any of the legacy `status` values
// (`queued`, `1`) returns true on success.
func (s *EmailService) Enqueue(ctx context.Context, in EmailEnqueueInput, opts ...CallOption) (bool, error) {
	if len(in.To) == 0 {
		return false, errors.New("account: Email.Enqueue requires at least one recipient in To")
	}
	if in.Subject == "" {
		return false, errors.New("account: Email.Enqueue requires Subject")
	}
	if in.Body == "" {
		return false, errors.New("account: Email.Enqueue requires Body")
	}
	wire := map[string]any{
		"to":      flattenRecipients(in.To),
		"subject": in.Subject,
		"body":    in.Body,
	}
	if len(in.Attachments) > 0 {
		wire["attachments"] = in.Attachments
	}
	bodyBytes, err := json.Marshal(wire)
	if err != nil {
		return false, fmt.Errorf("account: Email.Enqueue marshal: %w", err)
	}
	co := collectCallOptions(opts)
	if _, err := s.client.signedDo(ctx, callArgs{
		method:         http.MethodPost,
		path:           emailEnqueuePath,
		body:           bodyBytes,
		idempotencyKey: co.idempotencyKey,
	}); err != nil {
		return false, fmt.Errorf("account: Email.Enqueue: %w", err)
	}
	return true, nil
}

// flattenRecipients keeps the wire shape close to the TS version: a
// single recipient is sent as a bare string; multiple recipients are
// sent as an array.
func flattenRecipients(to []string) any {
	if len(to) == 1 {
		return to[0]
	}
	return to
}
