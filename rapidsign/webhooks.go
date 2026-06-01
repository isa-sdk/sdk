package rapidsign

import "net/http"

// WebhookEvent is the canonical decoded event payload. Fields are
// reserved for the upcoming webhook surface (#38); the type ships
// today so consumers can write code against it.
type WebhookEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	SignID    string         `json:"sign_id"`
	Status    DocumentStatus `json:"status"`
	CreatedAt int64          `json:"created_at"`
}

// Webhooks groups webhook-related helpers on a Client. The Verify
// method is reserved for the upcoming HMAC signature surface and
// returns *NotImplementedError until that surface lands.
type Webhooks struct {
	c *Client
}

// Verify will check the HMAC signature on a raw webhook body, parse
// the JSON envelope, and return the typed event. The server-side
// signing surface is still being designed (#38); for now this method
// returns *NotImplementedError so SDK consumers can wire the call
// site against the eventual shape.
func (w *Webhooks) Verify(rawBody []byte, headers http.Header, secret string) (*WebhookEvent, error) {
	return nil, &NotImplementedError{
		Err: &Error{
			Code:       ErrorCodeNotImplemented,
			HTTPStatus: http.StatusNotImplemented,
			Detail:     "rapidsign: Webhooks.Verify is not yet supported (tracked in #38). The SDK surface is reserved.",
		},
	}
}
