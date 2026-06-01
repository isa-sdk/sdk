package algosure

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// algosureInputs holds the parsed headers needed for signature verification
// and replay bookkeeping.
type algosureInputs struct {
	host       string
	tsStr      string
	sessionID  string
	authHeader string
	tsMillis   int64
	saltID     int64
}

// parseAlgosureRequest extracts and validates the Algosure headers. Returns
// an error when any required field is missing or the timestamp is malformed.
//
// *SaltId tells the verifier which row in proxy_salts the client signed
// against. net/http canonicalizes header names (CanonicalMIMEHeaderKey), so
// a single Header.Get(headerSaltID) matches whatever casing the client or
// API Gateway used. A missing or non-numeric value yields ErrMissingSaltID
// so callers map it to a 400 (request-shape) rather than a 401 (auth failure).
func parseAlgosureRequest(r *http.Request) (algosureInputs, error) {
	in := algosureInputs{
		host:       r.Header.Get(headerHost),
		tsStr:      r.Header.Get(headerTimestamp),
		sessionID:  r.Header.Get(headerSessionID),
		authHeader: r.Header.Get(headerAuth),
	}
	if in.host == "" || in.tsStr == "" || in.authHeader == "" {
		return algosureInputs{}, errors.New("algosure: missing required headers (*Host, *Timestamp, Authorization)")
	}
	if in.sessionID == "" {
		return algosureInputs{}, errors.New("algosure: missing required header *sessionId")
	}
	ts, err := strconv.ParseInt(in.tsStr, 10, 64)
	if err != nil {
		return algosureInputs{}, fmt.Errorf("algosure: invalid *Timestamp %q: %w", in.tsStr, err)
	}
	in.tsMillis = ts

	saltIDStr := r.Header.Get(headerSaltID)
	if saltIDStr == "" {
		return algosureInputs{}, ErrMissingSaltID
	}
	saltID, err := strconv.ParseInt(saltIDStr, 10, 64)
	if err != nil || saltID <= 0 {
		return algosureInputs{}, ErrMissingSaltID
	}
	in.saltID = saltID
	return in, nil
}
