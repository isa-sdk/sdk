package account

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// Zero-knowledge case crypto envelope, byte-compatible with the TypeScript
// SDK's caseCrypto.ts. The platform stores opaque ciphertext and never holds
// a key: the SDK generates a fresh data key per case, encrypts the payload
// with AES-GCM (the cleartext product tag is bound as additional authenticated
// data), and carries the key only in the share-link fragment.
//
// The wire contract splits the GCM auth tag out of the ciphertext (Go's
// cipher.AEAD.Seal appends it, like WebCrypto), mirroring the TS envelope's
// separate ciphertext / iv / tag fields.

const (
	// caseKeyBytes is the AES-128 data-key length used for fresh case keys.
	// Decrypt is length-agnostic (AES picks 128/192/256 from key length), so
	// 256-bit keys from earlier envelopes still open; this governs generation.
	caseKeyBytes = 16
	// caseIVBytes is the AES-GCM nonce length (96-bit, the GCM-recommended size).
	caseIVBytes = 12
	// caseTagBytes is the AES-GCM authentication-tag length (128-bit).
	caseTagBytes = 16
)

// CaseEnvelope is the opaque crypto payload the server stores, all standard
// (padded) base64. It mirrors the TypeScript TCaseEnvelope wire shape.
type CaseEnvelope struct {
	// Ciphertext is the std-base64 AES-GCM ciphertext with the tag stripped.
	Ciphertext string `json:"ciphertext"`
	// IV is the std-base64 AES-GCM nonce.
	IV string `json:"iv"`
	// Tag is the std-base64 AES-GCM authentication tag.
	Tag string `json:"tag"`
}

// EncryptedCase is the result of EncryptCase: the wire envelope plus the
// base64url fragment key destined for the share link's `#k=` fragment.
type EncryptedCase struct {
	// Envelope holds the base64 fields posted to /v1/case.
	Envelope CaseEnvelope
	// KeyFragment is the data key, base64url-encoded (no padding), for the link.
	KeyFragment string
}

// RandomBytes is the CSPRNG facade for case-key and nonce generation. Tests
// inject a deterministic source; production uses SystemRandomBytes.
type RandomBytes func(n int) ([]byte, error)

// SystemRandomBytes reads n cryptographically random bytes from crypto/rand.
// It is the only place in the case-crypto path that touches the OS RNG, so
// EncryptCase stays testable through the RandomBytes facade.
func SystemRandomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("account: read %d random bytes: %w", n, err)
	}
	return buf, nil
}

// ErrCaseDecrypt is returned when an envelope fails AES-GCM authentication:
// a tampered, corrupt, or product-mismatched payload, or a wrong fragment
// key. The recipient cannot recover the plaintext; treat it as terminal.
var ErrCaseDecrypt = errors.New("account: case envelope failed authentication (wrong key, wrong product, or tampered ciphertext)")

// EncryptCase encrypts a JSON payload under a fresh 128-bit key, binding
// product as AEAD additional data. It returns the base64 wire envelope and
// the base64url fragment key. The key never leaves this call except as the
// returned fragment value — the caller must keep it out of logs and telemetry.
func EncryptCase(product string, payload any, random RandomBytes) (*EncryptedCase, error) {
	if random == nil {
		random = SystemRandomBytes
	}
	rawKey, err := random(caseKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("account: EncryptCase generate key: %w", err)
	}
	iv, err := random(caseIVBytes)
	if err != nil {
		return nil, fmt.Errorf("account: EncryptCase generate nonce: %w", err)
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("account: EncryptCase marshal payload: %w", err)
	}
	gcm, err := newCaseGCM(rawKey)
	if err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, iv, plaintext, []byte(product))
	splitAt := len(sealed) - caseTagBytes
	return &EncryptedCase{
		Envelope: CaseEnvelope{
			Ciphertext: base64.StdEncoding.EncodeToString(sealed[:splitAt]),
			IV:         base64.StdEncoding.EncodeToString(iv),
			Tag:        base64.StdEncoding.EncodeToString(sealed[splitAt:]),
		},
		KeyFragment: base64.RawURLEncoding.EncodeToString(rawKey),
	}, nil
}

// DecryptCase decrypts a wire envelope with the fragment key, verifying the
// product AEAD binding, and returns the parsed JSON payload. It wraps
// ErrCaseDecrypt on any authentication failure. The fragment key may be
// base64url (the share-link form) or standard base64; both decode.
func DecryptCase(product string, envelope CaseEnvelope, keyFragment string) (any, error) {
	rawKey, err := decodeFragmentKey(keyFragment)
	if err != nil {
		return nil, fmt.Errorf("account: DecryptCase decode key: %w", err)
	}
	iv, err := base64.StdEncoding.DecodeString(envelope.IV)
	if err != nil {
		return nil, fmt.Errorf("account: DecryptCase decode iv: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("account: DecryptCase decode ciphertext: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(envelope.Tag)
	if err != nil {
		return nil, fmt.Errorf("account: DecryptCase decode tag: %w", err)
	}
	gcm, err := newCaseGCM(rawKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, iv, append(ciphertext, tag...), []byte(product))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCaseDecrypt, err)
	}
	var payload any
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("account: DecryptCase parse plaintext: %w", err)
	}
	return payload, nil
}

// newCaseGCM builds an AES-GCM AEAD with the GCM-standard 96-bit nonce and
// 128-bit tag from the raw key, selecting AES-128/192/256 by key length.
func newCaseGCM(rawKey []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(rawKey)
	if err != nil {
		return nil, fmt.Errorf("account: build AES cipher (key length %d): %w", len(rawKey), err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, caseIVBytes)
	if err != nil {
		return nil, fmt.Errorf("account: build AES-GCM: %w", err)
	}
	return gcm, nil
}

// decodeFragmentKey accepts both the base64url fragment form (the share-link
// encoding, with or without padding) and standard base64, matching the TS
// decoder that normalizes the URL-safe alphabet before decoding.
func decodeFragmentKey(fragment string) ([]byte, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(fragment); err == nil {
		return raw, nil
	}
	if raw, err := base64.URLEncoding.DecodeString(fragment); err == nil {
		return raw, nil
	}
	raw, err := base64.StdEncoding.DecodeString(fragment)
	if err != nil {
		return nil, fmt.Errorf("decode fragment key %q: %w", fragment, err)
	}
	return raw, nil
}
