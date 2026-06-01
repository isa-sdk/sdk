// Package rapidsign is the Stripe-quality Go client for the RapidSign
// document-signing surface (rapidsign.isaapi.com). The public surface is
// intentionally narrower than the wire protocol: callers describe what
// they want ("send this packet, await signature, download the result"),
// and the client hides the staged CreateDocument+NotifyDocument call
// pair, session-id minting, idempotency-key generation, polling
// backoff, and gzip transport.
//
// Request methods take a context.Context as their first parameter, return
// typed errors that satisfy error and unwrap via errors.As. Retry hints
// are available on RateLimitedError via Err.RetryAfter.
package rapidsign

import "time"

// DocumentStatus is the lifecycle state of a signing envelope.
//
// It mirrors the api.rapidsign.v1.DocumentStatus enum but uses string
// constants on the wire so future server additions do not require a
// client release.
type DocumentStatus string

// Documented lifecycle states. Servers MAY return values not in this
// set; consumers should compare via the constants but treat unknown
// strings as forward-compatible (do not panic).
const (
	DocumentStatusUnspecified DocumentStatus = ""
	DocumentStatusPending     DocumentStatus = "pending"
	DocumentStatusSaved       DocumentStatus = "saved"
	DocumentStatusNotified    DocumentStatus = "notified"
)

// Recipient identifies who will sign the envelope and where the signer
// notification email is dispatched. Name is optional and used as the
// To-header display name.
type Recipient struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// PdfSource describes one input PDF the renderer must fetch and merge
// into the packet. ExpectedHash is optional; when set the renderer
// aborts with a validation error on hash mismatch.
type PdfSource struct {
	URL          string `json:"url"`
	ExpectedHash string `json:"expected_hash,omitempty"`
}

// SendRequest is the input to Documents.Send.
//
// LegalText is embedded beneath the signature block when non-empty. TTL
// follows the ISO-8601 duration format used by the wire (`P30D`); an
// empty value applies the service default. Metadata is opaque to the
// client and stored verbatim for the audit trail.
type SendRequest struct {
	Packet        []PdfSource       `json:"packet"`
	Recipient     Recipient         `json:"recipient"`
	LegalText     string            `json:"legal_text,omitempty"`
	TTL           string            `json:"ttl,omitempty"`
	UserAgent     string            `json:"user_agent,omitempty"`
	IsProduction  bool              `json:"is_production,omitempty"`
	RemoteAllowed *bool             `json:"remote_allowed,omitempty"`
	TemplateData  map[string]any    `json:"data,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// CancelRequest captures the human-readable reason a signing flow was
// abandoned. The reason is persisted on the audit trail when the server
// surface lands; for now Cancel returns NotImplementedError.
type CancelRequest struct {
	Reason string `json:"reason,omitempty"`
}

// AwaitOpts configures Documents.AwaitSignature. Timeout caps the total
// wall-clock duration the polling loop will wait; the loop also honors
// ctx.Done() and returns the wrapped context error on cancellation.
type AwaitOpts struct {
	Timeout time.Duration
}

// Envelope is the canonical view of a signing transaction returned by
// Send/Get/AwaitSignature.
//
// ID identifies the document; SignID identifies the signature slot
// (matches the wire field). Hashes maps each source URL the renderer
// fetched to the SHA-256 it observed. CreatedAt and ExpiresAt are
// derived from the TTL applied at render time.
type Envelope struct {
	ID         string            `json:"id"`
	SignID     string            `json:"sign_id"`
	SignURL    string            `json:"sign_url"`
	ViewURL    string            `json:"view_url"`
	Status     DocumentStatus    `json:"status"`
	Recipient  Recipient         `json:"recipient"`
	Hashes     map[string]string `json:"hashes,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	ExpiresAt  time.Time         `json:"expires_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	LegalText  string            `json:"legal_text,omitempty"`
}

// Signature is the captured signing event. Signature is the
// already-base64-decoded image bytes; SignedAt is server-stamped.
type Signature struct {
	SignID    string            `json:"sign_id"`
	Signature []byte            `json:"-"`
	SignedAt  time.Time         `json:"signed_at"`
	SignerIP  string            `json:"signer_ip,omitempty"`
	UserAgent string            `json:"user_agent,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
