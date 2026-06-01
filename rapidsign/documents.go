package rapidsign

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/isa-sdk/sdk/rapidsign/internal"
)

// Documents groups the document-lifecycle operations on a Client.
//
// All methods take a context.Context as the first parameter, return
// the canonical Envelope/Signature value types, and surface typed
// errors via errors.As. Idempotency keys and session ids are minted
// internally — callers describe what they want, not how the wire is
// shaped.
type Documents struct {
	c *Client
}

// SendOptions overrides the auto-generated request metadata. Production
// callers leave this empty; integrators that already issued an
// idempotency key for at-least-once delivery from a queue pass it here.
type SendOptions struct {
	IdempotencyKey string
	SessionID      string
}

// Send renders a document packet, stores it, and notifies the recipient
// in a single call. Internally this issues POST /v1/documents followed
// by POST /v1/documents/{sign_id}/notify; both must succeed for Send
// to return a populated Envelope. If notify fails after create
// succeeded the Envelope is still returned (status pending) alongside a
// wrapped error so callers can decide whether to resend the notice.
//
// Proto follow-up tracked in #38: collapse to a single /v1/documents
// endpoint server-side so the SDK can drop the staged-call dance.
func (d *Documents) Send(ctx context.Context, req *SendRequest, opts ...SendOptions) (*Envelope, error) {
	if req == nil {
		return nil, fmt.Errorf("rapidsign: Documents.Send requires a non-nil SendRequest")
	}
	if len(req.Packet) == 0 {
		return nil, fmt.Errorf("rapidsign: Documents.Send requires at least one PdfSource")
	}
	if req.Recipient.Email == "" {
		return nil, fmt.Errorf("rapidsign: Documents.Send requires a recipient email")
	}

	var sendOpts SendOptions
	if len(opts) > 0 {
		sendOpts = opts[0]
	}

	sessionID := sendOpts.SessionID
	if sessionID == "" {
		s, err := d.c.ids.NewSessionID()
		if err != nil {
			return nil, err
		}
		sessionID = s
	}

	idempotencyKey := sendOpts.IdempotencyKey
	if idempotencyKey == "" {
		k, err := d.c.ids.NewUUIDv4()
		if err != nil {
			return nil, err
		}
		idempotencyKey = k
	}

	created, err := d.create(ctx, req, sessionID, idempotencyKey)
	if err != nil {
		return nil, err
	}

	notifyKey, err := d.c.ids.NewUUIDv4()
	if err != nil {
		// Create succeeded; surface the envelope so caller can retry
		// notify out-of-band.
		return created, fmt.Errorf("rapidsign: Documents.Send notify-key generation failed after create succeeded: %w", err)
	}

	if err := d.notify(ctx, created.SignID, sessionID, req.Recipient.Email, notifyKey); err != nil {
		return created, fmt.Errorf("rapidsign: Documents.Send notify step failed after create succeeded (sign_id=%s): %w", created.SignID, err)
	}
	created.Status = DocumentStatusNotified
	return created, nil
}

// createDocumentRequest is the on-wire body for POST /v1/documents.
// The exported SendRequest is the user-facing shape; we translate to
// this struct so callers do not see wire-only fields (sign_ids,
// view_only_id, packet_stored).
type createDocumentRequest struct {
	SessionID        string            `json:"session_id"`
	Packet           []PdfSource       `json:"packet"`
	Data             map[string]any    `json:"data,omitempty"`
	RemoteAllowed    bool              `json:"remote_allowed"`
	UserAgent        string            `json:"user_agent,omitempty"`
	IsProduction     bool              `json:"is_production,omitempty"`
	BindingLegalText string            `json:"binding_legal_text,omitempty"`
	TTL              string            `json:"ttl,omitempty"`
	ExpectedHashes   map[string]string `json:"expected_hashes,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// createDocumentResponseEnvelope captures the success body shape after
// envelope unwrapping. The server returns sign_ids[] but the SDK
// surfaces a singular SignID — we pick the first id, matching the
// single-recipient flow Send describes.
type createDocumentResponseEnvelope struct {
	ID           string            `json:"id"`
	SignIDs      []string          `json:"sign_ids"`
	SignID       string            `json:"sign_id"`
	SignURL      string            `json:"sign_url"`
	ViewURL      string            `json:"view_url"`
	ViewOnlyID   string            `json:"view_only_id"`
	Status       DocumentStatus    `json:"status"`
	Hashes       map[string]string `json:"hashes"`
	PacketStored bool              `json:"packet_stored"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

func (d *Documents) create(ctx context.Context, req *SendRequest, sessionID, idempotencyKey string) (*Envelope, error) {
	remoteAllowed := true
	if req.RemoteAllowed != nil {
		remoteAllowed = *req.RemoteAllowed
	}
	body := createDocumentRequest{
		SessionID:        sessionID,
		Packet:           req.Packet,
		Data:             req.TemplateData,
		RemoteAllowed:    remoteAllowed,
		UserAgent:        req.UserAgent,
		IsProduction:     req.IsProduction,
		BindingLegalText: req.LegalText,
		TTL:              req.TTL,
		ExpectedHashes:   expectedHashesFromPacket(req.Packet),
		Metadata:         req.Metadata,
	}

	resp, err := internal.JSONRequest(ctx, d.c.doer, http.MethodPost, d.c.baseURL+"/v1/documents", body, internal.RequestOptions{
		IdempotencyKey: idempotencyKey,
		Headers:        map[string]string{"User-Agent": d.c.userAgent},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, parseErrorResponse(resp, d.c.clock())
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	var payload createDocumentResponseEnvelope
	if err := decodeEnvelope(resp.Body, &payload); err != nil {
		return nil, err
	}

	env := &Envelope{
		ID:        payload.ID,
		SignID:    pickSignID(payload),
		SignURL:   payload.SignURL,
		ViewURL:   payload.ViewURL,
		Status:    fallbackStatus(payload.Status, DocumentStatusPending),
		Recipient: req.Recipient,
		Hashes:    payload.Hashes,
		CreatedAt: payload.CreatedAt,
		ExpiresAt: payload.ExpiresAt,
		Metadata:  req.Metadata,
		LegalText: req.LegalText,
	}
	if env.SignID == "" {
		return nil, fmt.Errorf("rapidsign: server response missing sign_id (request_id=%s)", resp.Header.Get("X-Request-ID"))
	}
	return env, nil
}

// pickSignID prefers the singular sign_id when present, falling back to
// the first element of sign_ids. The proto exposes both today; the SDK
// surface promises only one.
// expectedHashesFromPacket builds the top-level expected_hashes map from
// per-source PdfSource.ExpectedHash values. Keys are source URLs.
func expectedHashesFromPacket(packet []PdfSource) map[string]string {
	hashes := make(map[string]string)
	for _, src := range packet {
		if src.ExpectedHash == "" || src.URL == "" {
			continue
		}
		hashes[src.URL] = src.ExpectedHash
	}
	if len(hashes) == 0 {
		return nil
	}
	return hashes
}

func pickSignID(p createDocumentResponseEnvelope) string {
	if p.SignID != "" {
		return p.SignID
	}
	if len(p.SignIDs) > 0 {
		return p.SignIDs[0]
	}
	return ""
}

func fallbackStatus(s, fallback DocumentStatus) DocumentStatus {
	if s == "" {
		return fallback
	}
	return s
}

type notifyDocumentRequest struct {
	SessionID string `json:"session_id"`
	To        string `json:"to"`
	Key       string `json:"key,omitempty"`
}

func (d *Documents) notify(ctx context.Context, signID, sessionID, to, idempotencyKey string) error {
	body := notifyDocumentRequest{SessionID: sessionID, To: to}
	url := fmt.Sprintf("%s/v1/documents/%s/notify", d.c.baseURL, signID)
	resp, err := internal.JSONRequest(ctx, d.c.doer, http.MethodPost, url, body, internal.RequestOptions{
		IdempotencyKey: idempotencyKey,
		Headers:        map[string]string{"User-Agent": d.c.userAgent},
	})
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return parseErrorResponse(resp, d.c.clock())
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

// getDocumentResponse is the wire shape for GET /v1/documents/{sign_id}.
// The SDK exposes Signature instead — we translate base64 + timestamp
// to []byte + time.Time at the boundary.
type getDocumentResponse struct {
	SignID    string `json:"sign_id"`
	Signature string `json:"signature"`
	Metadata  string `json:"user_metadata"`
	Timestamp int64  `json:"timestamp"`
	Status    string `json:"status"`
	SignURL   string `json:"sign_url"`
	ViewURL   string `json:"view_url"`
	SignerIP  string `json:"signer_ip,omitempty"`
}

// Get returns the current state of a signing envelope. When the
// envelope has not been signed yet the server returns 404; callers
// switch on *NotFoundError via errors.As to handle that case.
func (d *Documents) Get(ctx context.Context, signID string) (*Signature, error) {
	if signID == "" {
		return nil, fmt.Errorf("rapidsign: Documents.Get requires a non-empty sign id")
	}
	url := fmt.Sprintf("%s/v1/documents/%s", d.c.baseURL, signID)
	resp, err := internal.JSONRequest(ctx, d.c.doer, http.MethodGet, url, nil, internal.RequestOptions{
		Headers: map[string]string{"User-Agent": d.c.userAgent},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, parseErrorResponse(resp, d.c.clock())
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	var payload getDocumentResponse
	if err := decodeEnvelope(resp.Body, &payload); err != nil {
		return nil, err
	}

	sig := &Signature{
		SignID:   payload.SignID,
		SignedAt: time.Unix(payload.Timestamp, 0).UTC(),
		SignerIP: payload.SignerIP,
	}
	if payload.Signature != "" {
		decoded, err := decodeBase64Signature(payload.Signature)
		if err != nil {
			return nil, err
		}
		sig.Signature = decoded
	}
	if payload.Metadata != "" {
		// Best-effort parse: server stores opaque metadata as a JSON
		// string per the proto; surface it as a map when shaped that
		// way, and ignore otherwise (raw string is dropped — callers
		// needing the raw form should fall back to the wire types).
		_ = json.Unmarshal([]byte(payload.Metadata), &sig.Metadata)
	}
	return sig, nil
}

// downloadDocumentResponse is the wire shape for GET /v1/documents/{sign_id}/download.
type downloadDocumentResponse struct {
	SignID           string `json:"sign_id"`
	PDFGzipBase64    string `json:"pdf_gzip_base64"`
	Compressed       bool   `json:"compressed"`
	BindingLegalText string `json:"binding_legal_text"`
	SizeBytes        int64  `json:"size_bytes"`
}

// Download fetches the signed PDF for sign_id and returns it as raw
// (decompressed) bytes. The on-wire payload is gzipped+base64; both
// layers are stripped here so callers can write the result directly to
// disk or hand it to a PDF library.
func (d *Documents) Download(ctx context.Context, signID string) ([]byte, error) {
	if signID == "" {
		return nil, fmt.Errorf("rapidsign: Documents.Download requires a non-empty sign id")
	}
	url := fmt.Sprintf("%s/v1/documents/%s/download", d.c.baseURL, signID)
	resp, err := internal.JSONRequest(ctx, d.c.doer, http.MethodGet, url, nil, internal.RequestOptions{
		Headers: map[string]string{"User-Agent": d.c.userAgent},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, parseErrorResponse(resp, d.c.clock())
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	var payload downloadDocumentResponse
	if err := decodeEnvelope(resp.Body, &payload); err != nil {
		return nil, err
	}
	if payload.PDFGzipBase64 == "" {
		return nil, fmt.Errorf("rapidsign: Documents.Download received empty payload for sign_id=%s", signID)
	}
	if !payload.Compressed {
		// Reserved-for-future per proto; if a server ever emits
		// uncompressed, treat the base64 as direct PDF bytes.
		return decodeBase64Signature(payload.PDFGzipBase64)
	}
	return internal.DecodeGzippedBase64(payload.PDFGzipBase64)
}

// Cancel is reserved for the future server-side cancel endpoint
// (tracked in #38). Today it returns *NotImplementedError so callers
// can ship code against the eventual surface without conditional
// dispatch.
func (d *Documents) Cancel(ctx context.Context, signID string, req *CancelRequest) error {
	if signID == "" {
		return fmt.Errorf("rapidsign: Documents.Cancel requires a non-empty sign id")
	}
	return &NotImplementedError{
		Err: &Error{
			Code:       ErrorCodeNotImplemented,
			HTTPStatus: http.StatusNotImplemented,
			Detail:     "rapidsign: Documents.Cancel is not yet supported by the server (tracked in #38). The SDK surface is reserved.",
		},
	}
}

// AwaitSignature polls Documents.Get until the envelope is signed or
// the supplied timeout / parent context fires. Polling uses jittered
// exponential backoff between pollInitialInterval and pollMaxInterval.
//
// Returns the populated Signature on success, *NotFoundError when the
// sign_id is unknown, or a context-derived error on cancellation /
// timeout. The polling cadence is intentionally not configurable —
// callers describe how long they will wait, not how aggressively to
// hammer the server.
func (d *Documents) AwaitSignature(ctx context.Context, signID string, opts AwaitOpts) (*Signature, error) {
	if signID == "" {
		return nil, fmt.Errorf("rapidsign: Documents.AwaitSignature requires a non-empty sign id")
	}
	deadline := time.Time{}
	if opts.Timeout > 0 {
		deadline = d.c.clock().Add(opts.Timeout)
	}

	interval := pollInitialInterval
	stop := ctx.Done()
	for {
		now := d.c.clock()
		if !deadline.IsZero() && !now.Before(deadline) {
			return nil, awaitSignatureTimeoutError(opts.Timeout, signID)
		}

		pollCtx := ctx
		var cancelPoll context.CancelFunc
		if !deadline.IsZero() {
			pollCtx, cancelPoll = context.WithTimeout(ctx, deadline.Sub(now))
		}
		sig, err := d.Get(pollCtx, signID)
		if cancelPoll != nil {
			cancelPoll()
		}
		if err == nil && len(sig.Signature) > 0 {
			return sig, nil
		}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || pollCtx.Err() == context.DeadlineExceeded {
				if !deadline.IsZero() {
					return nil, awaitSignatureTimeoutError(opts.Timeout, signID)
				}
				return nil, fmt.Errorf(
					"rapidsign: AwaitSignature cancelled while waiting on sign_id=%s: %w",
					signID, ctx.Err(),
				)
			}
			// A 404 here means "not signed yet" given Send was called
			// against this sign_id; treat as a poll miss, not a terminal
			// error. Unknown sign_ids supplied by the caller will continue
			// to poll until the deadline — that is the correct trade since
			// the SDK cannot distinguish.
			if !isPollMiss(err) {
				return nil, err
			}
		}

		now = d.c.clock()
		if !deadline.IsZero() && !now.Before(deadline) {
			return nil, awaitSignatureTimeoutError(opts.Timeout, signID)
		}
		wait := pollIntervalWithJitter(interval, d.c.ids.Reader)
		if !deadline.IsZero() {
			if remaining := deadline.Sub(now); remaining < wait {
				wait = remaining
			}
		}
		if completed := d.c.sleeper.Sleep(stop, wait); !completed {
			return nil, fmt.Errorf("rapidsign: AwaitSignature cancelled while waiting on sign_id=%s: %w", signID, ctx.Err())
		}
		interval *= 2
		if interval > pollMaxInterval {
			interval = pollMaxInterval
		}
	}
}

func awaitSignatureTimeoutError(timeout time.Duration, signID string) error {
	return fmt.Errorf(
		"rapidsign: AwaitSignature timed out after %s waiting on sign_id=%s: %w",
		timeout, signID, context.DeadlineExceeded,
	)
}

// pollIntervalWithJitter adds up to pollJitterFraction of random delay so
// concurrent pollers do not realign on the same cadence.
func pollIntervalWithJitter(base time.Duration, r io.Reader) time.Duration {
	if base <= 0 || r == nil {
		return base
	}
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return base
	}
	maxJitter := uint64(base) * pollJitterNumerator / pollJitterDenominator
	if maxJitter == 0 {
		return base
	}
	return base + time.Duration(binary.BigEndian.Uint64(buf[:])%maxJitter)
}

// isPollMiss reports whether err is a transient "not signed yet"
// response we should keep polling on.
func isPollMiss(err error) bool {
	var nfe *NotFoundError
	return errorsAs(err, &nfe)
}
