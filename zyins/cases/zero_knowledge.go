package cases

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// dataKeyBytes is the AES-256-GCM data-key length in bytes.
const dataKeyBytes = 32

// gcmNonceBytes is the AES-GCM nonce length in bytes (96-bit, the
// GCM-recommended size).
const gcmNonceBytes = 12

// casesWirePath is the platform's case storage surface. The default
// pins to the /v1/cases write/read shape mirrored from the TS account
// store; surface drift is handled at the parent SDK layer via the
// per-surface APIVersion map.
const (
	casesPutPath = "/v1/cases"
	casesGetPath = "/v1/cases/" // id appended at call time
)

// putWireBody is the JSON shape POSTed to /v1/cases. The platform
// stores opaque ciphertext keyed by Product; nothing in the body
// reveals the cleartext payload.
type putWireBody struct {
	Product    string            `json:"product"`
	Ciphertext string            `json:"ciphertext"`
	IV         string            `json:"iv"`
	Tag        string            `json:"tag"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ID         string            `json:"id,omitempty"`
}

// putWireResponse is the envelope returned by /v1/cases. ID is the
// server-assigned identifier; subsequent Get calls reference it.
type putWireResponse struct {
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
}

// getWireResponse mirrors the GET /v1/cases/{id} envelope shape.
type getWireResponse struct {
	Data struct {
		Product    string            `json:"product"`
		Ciphertext string            `json:"ciphertext"`
		IV         string            `json:"iv"`
		Tag        string            `json:"tag"`
		Metadata   map[string]string `json:"metadata,omitempty"`
	} `json:"data"`
}

// ZeroKnowledgeCaseStorage encrypts record bodies with AES-256-GCM
// client-side, posts only the ciphertext envelope to the platform,
// and returns the data key in the [PutResult.RecallToken]. The
// platform never holds the key — recall requires the caller to
// supply it via [Get].
//
// The product tag is bound as AES-GCM additional authenticated data;
// records encrypted under one product cannot be authenticated under
// another even with a matching key.
type ZeroKnowledgeCaseStorage struct {
	doer Doer
	// randSource is the entropy source for keys and nonces. nil
	// resolves to crypto/rand.Reader at call time.
	randSource io.Reader
}

// NewZeroKnowledgeCaseStorage constructs the default storage. The
// supplied [Doer] provides the wire transport; the parent SDK wires
// one wrapping *zyins.Client when the caller does not override
// CaseStorage.
func NewZeroKnowledgeCaseStorage(doer Doer) *ZeroKnowledgeCaseStorage {
	return &ZeroKnowledgeCaseStorage{doer: doer}
}

// Put encrypts record.Body under a fresh data key, posts the
// ciphertext envelope, and returns the server id + the recall token
// (base64url-encoded data key). The cleartext payload never leaves
// the process.
func (s *ZeroKnowledgeCaseStorage) Put(ctx context.Context, record CaseRecord) (PutResult, error) {
	if s.doer == nil {
		return PutResult{}, errors.New("cases: ZeroKnowledgeCaseStorage missing transport")
	}
	if record.Product == "" {
		return PutResult{}, errors.New("cases: CaseRecord.Product is required")
	}
	dataKey, nonce, err := s.generateKeyAndNonce()
	if err != nil {
		return PutResult{}, fmt.Errorf("cases: generate key for product %q: %w", record.Product, err)
	}
	ciphertext, tag, err := sealRecord(dataKey, nonce, record.Product, record.Body)
	if err != nil {
		return PutResult{}, fmt.Errorf("cases: seal record for product %q: %w", record.Product, err)
	}
	body := putWireBody{
		Product:    record.Product,
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		IV:         base64.StdEncoding.EncodeToString(nonce),
		Tag:        base64.StdEncoding.EncodeToString(tag),
		Metadata:   record.Metadata,
		ID:         record.ID,
	}
	raw, err := s.doer.Post(ctx, casesPutPath, body)
	if err != nil {
		return PutResult{}, fmt.Errorf("cases: POST %s: %w", casesPutPath, err)
	}
	var decoded putWireResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return PutResult{}, fmt.Errorf("cases: decode put response: %w", err)
	}
	id := decoded.Data.ID
	if id == "" {
		id = record.ID
	}
	return PutResult{
		ID:          id,
		RecallToken: base64.RawURLEncoding.EncodeToString(dataKey),
	}, nil
}

// Get fetches the envelope identified by id and decrypts it with the
// supplied recall token. Returns [ErrNotFound] on a 404 or when the
// recall token does not authenticate the ciphertext.
func (s *ZeroKnowledgeCaseStorage) Get(ctx context.Context, id, recallToken string) (CaseRecord, error) {
	if s.doer == nil {
		return CaseRecord{}, errors.New("cases: ZeroKnowledgeCaseStorage missing transport")
	}
	if id == "" {
		return CaseRecord{}, errors.New("cases: Get requires a non-empty id")
	}
	if recallToken == "" {
		return CaseRecord{}, errors.New("cases: Get requires a non-empty recall token")
	}
	dataKey, err := base64.RawURLEncoding.DecodeString(recallToken)
	if err != nil {
		return CaseRecord{}, fmt.Errorf("cases: decode recall token: %w", err)
	}
	raw, err := s.doer.Get(ctx, casesGetPath+id)
	if err != nil {
		if isNotFound(err) {
			return CaseRecord{}, ErrNotFound
		}
		return CaseRecord{}, fmt.Errorf("cases: GET %s%s: %w", casesGetPath, id, err)
	}
	var decoded getWireResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return CaseRecord{}, fmt.Errorf("cases: decode get response: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(decoded.Data.Ciphertext)
	if err != nil {
		return CaseRecord{}, fmt.Errorf("cases: decode ciphertext: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(decoded.Data.IV)
	if err != nil {
		return CaseRecord{}, fmt.Errorf("cases: decode iv: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(decoded.Data.Tag)
	if err != nil {
		return CaseRecord{}, fmt.Errorf("cases: decode tag: %w", err)
	}
	plaintext, err := openRecord(dataKey, nonce, decoded.Data.Product, ciphertext, tag)
	if err != nil {
		return CaseRecord{}, ErrNotFound
	}
	return CaseRecord{
		ID:       id,
		Product:  decoded.Data.Product,
		Body:     plaintext,
		Metadata: decoded.Data.Metadata,
	}, nil
}

// generateKeyAndNonce produces a fresh AES-256 key and 96-bit nonce.
// The random source defaults to crypto/rand.Reader; tests may override
// via the unexported randSource field.
func (s *ZeroKnowledgeCaseStorage) generateKeyAndNonce() ([]byte, []byte, error) {
	r := s.randSource
	if r == nil {
		r = rand.Reader
	}
	key := make([]byte, dataKeyBytes)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, nil, fmt.Errorf("read data key: %w", err)
	}
	nonce := make([]byte, gcmNonceBytes)
	if _, err := io.ReadFull(r, nonce); err != nil {
		return nil, nil, fmt.Errorf("read nonce: %w", err)
	}
	return key, nonce, nil
}

// sealRecord encrypts plaintext under key+nonce, binding product as
// additional authenticated data, and splits the GCM output into
// ciphertext + tag for the wire envelope shape.
func sealRecord(key, nonce []byte, product string, plaintext []byte) ([]byte, []byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, []byte(product))
	splitAt := len(sealed) - gcm.Overhead()
	return sealed[:splitAt], sealed[splitAt:], nil
}

// openRecord verifies ciphertext+tag under key+nonce with product as
// additional authenticated data and returns the plaintext.
func openRecord(key, nonce []byte, product string, ciphertext, tag []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	combined := make([]byte, 0, len(ciphertext)+len(tag))
	combined = append(combined, ciphertext...)
	combined = append(combined, tag...)
	return gcm.Open(nil, nonce, combined, []byte(product))
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cases: aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cases: cipher.NewGCM: %w", err)
	}
	return gcm, nil
}

// statusCoder is the slice of the transport error the cases package
// needs: the HTTP status of the failed response. The zyins *Error
// satisfies it (via StatusCode), so matching structurally keeps this
// package free of a hard dependency on the zyins error types.
type statusCoder interface {
	StatusCode() int
}

// isNotFound reports whether err represents an HTTP 404. It matches on
// the structured status code carried by the transport error, never on a
// substring of the message: a case id like "case-404" or a 500 whose
// body mentions "404" must not be misread as not-found, which would
// silently drop a real error as an absent record (data loss).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var sc statusCoder
	if errors.As(err, &sc) {
		return sc.StatusCode() == http.StatusNotFound
	}
	return false
}
