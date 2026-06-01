package algosure

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/isa-sdk/sdk/core/replay"
)

// computeBodyHashOfString hashes a body string directly. The production
// computeBodyHash reads from r.Body and restores it; this helper short-
// circuits that for table-driven vector tests where the body is in hand.
func computeBodyHashOfString(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// buildCanonical concatenates the canonical signing string in the order
// the verifier uses. Mirrors verifySignature's strings.Join call.
func buildCanonical(method, path, bodyHashHex, tsStr, sessionID string) string {
	return strings.Join([]string{method, path, bodyHashHex, tsStr, sessionID}, "\x00")
}

//go:embed testdata/algosure_vectors.json
var algosureVectorsJSON []byte

// parityVector mirrors the JSON shape emitted by
// shared/schemas/sdk/testdata/generate_vectors.mjs. Only the fields the
// test needs are unmarshaled; unknown fields are ignored.
type parityVector struct {
	Name     string `json:"name"`
	Verifier string `json:"verifier"`
	Inputs   struct {
		Method      string `json:"method"`
		Path        string `json:"path"`
		Body        string `json:"body"`
		Host        string `json:"host"`
		SessionID   string `json:"session_id"`
		SaltID      int64  `json:"salt_id"`
		SaltContent string `json:"salt_content"`
		TimestampMS int64  `json:"timestamp_ms"`
	} `json:"inputs"`
	Expected struct {
		BodyHashHex      string `json:"body_hash_hex"`
		Bucket           int64  `json:"bucket"`
		SimpleKeyHex     string `json:"simple_key_hex"`
		CanonicalHex     string `json:"canonical_hex"`
		AuthorizationHex string `json:"authorization_hex"`
		Headers          struct {
			Authorization string `json:"Authorization"`
			Host          string `json:"*Host"`
			Timestamp     string `json:"*Timestamp"`
			SessionID     string `json:"*sessionId"`
			SaltID        string `json:"*SaltId"`
		} `json:"headers"`
	} `json:"expected"`
}

type parityDoc struct {
	Vectors []parityVector `json:"vectors"`
}

func loadParityVectors(t *testing.T) []parityVector {
	t.Helper()
	var doc parityDoc
	if err := json.Unmarshal(algosureVectorsJSON, &doc); err != nil {
		t.Fatalf("unmarshal vectors: %v", err)
	}
	if len(doc.Vectors) < 7 {
		t.Fatalf("expected >= 7 vectors, got %d", len(doc.Vectors))
	}
	return doc.Vectors
}

// vectorSalt fulfils SaltFetcher by serving the vector's salt_content
// regardless of (saltID, host). The verifier still validates that the
// header is present; this fetcher simply skips the data-layer dimension
// because the parity vectors fix it as constant.
type vectorSalt struct{ content []byte }

func (v *vectorSalt) FetchByID(_ context.Context, _ int64, _ string) ([]byte, error) {
	return v.content, nil
}

type allowAllHosts struct{}

func (allowAllHosts) LookupHost(_ context.Context, _ string) (string, error) {
	return "acct_parity", nil
}

// TestAlgosureParity_PureComputation asserts the algorithm primitives in
// hmac.go match the cross-language vectors byte-for-byte. This is the
// signer-equivalent check: no HTTP, no verifier state, just the math.
func TestAlgosureParity_PureComputation(t *testing.T) {
	for _, v := range loadParityVectors(t) {
		t.Run(v.Name+"/computation", func(t *testing.T) {
			gotBodyHash := computeBodyHashOfString(v.Inputs.Body)
			if gotBodyHash != v.Expected.BodyHashHex {
				t.Fatalf("body hash mismatch:\n got  %s\n want %s", gotBodyHash, v.Expected.BodyHashHex)
			}

			var keyBuf [maxSimpleKeyLen]byte
			simpleKey := deriveSimpleKey(&keyBuf, []byte(v.Inputs.SaltContent), v.Inputs.TimestampMS)
			gotKeyHex := hex.EncodeToString(simpleKey)
			if gotKeyHex != v.Expected.SimpleKeyHex {
				t.Fatalf("simple key mismatch:\n got  %s\n want %s", gotKeyHex, v.Expected.SimpleKeyHex)
			}

			canonical := buildCanonical(v.Inputs.Method, v.Inputs.Path, gotBodyHash, v.Expected.Headers.Timestamp, v.Inputs.SessionID)
			gotCanonicalHex := hex.EncodeToString([]byte(canonical))
			if gotCanonicalHex != v.Expected.CanonicalHex {
				t.Fatalf("canonical mismatch:\n got  %s\n want %s", gotCanonicalHex, v.Expected.CanonicalHex)
			}

			gotMAC := computeHMAC(simpleKey, canonical)
			if gotMAC != v.Expected.AuthorizationHex {
				t.Fatalf("hmac mismatch:\n got  %s\n want %s", gotMAC, v.Expected.AuthorizationHex)
			}
		})
	}
}

// TestAlgosureParity_Verifier walks every vector through the production
// Verifier with a clock pinned to the vector's timestamp (so the pass
// vectors are inside the tolerance window). Negative vectors assert the
// matching error sentinel.
func TestAlgosureParity_Verifier(t *testing.T) {
	for _, v := range loadParityVectors(t) {
		t.Run(v.Name+"/verifier", func(t *testing.T) {
			cache, err := replay.NewInMemoryCache(replay.MemoryConfig{Window: 60 * time.Second})
			if err != nil {
				t.Fatalf("new memory cache: %v", err)
			}
			pinnedNow := time.UnixMilli(refNowForVector(v))
			ver, err := NewVerifier(Config{
				Hosts:  allowAllHosts{},
				Salt:   &vectorSalt{content: []byte(v.Inputs.SaltContent)},
				Replay: cache,
				Now:    func() time.Time { return pinnedNow },
			})
			if err != nil {
				t.Fatalf("new verifier: %v", err)
			}

			req := buildVectorRequest(v)
			applyVectorMutations(req, v)
			_, gotErr := ver.Verify(context.Background(), req)
			assertVectorOutcome(t, v, gotErr)
		})
	}
}

// refNowForVector returns the timestamp the verifier should treat as
// "now" when validating the vector. Pass vectors use the vector's own
// timestamp; the timestamp-drift negative case fixes "now" at T_NORMAL
// so the verifier sees a 90s-old request.
func refNowForVector(v parityVector) int64 {
	if v.Verifier == "reject_timestamp" {
		return 1_714_000_000_000
	}
	return v.Inputs.TimestampMS
}

func buildVectorRequest(v parityVector) *http.Request {
	r := httptest.NewRequest(v.Inputs.Method, v.Inputs.Path, strings.NewReader(v.Inputs.Body))
	r.Header.Set(headerAuth, v.Expected.Headers.Authorization)
	r.Header.Set(headerHost, v.Expected.Headers.Host)
	r.Header.Set(headerTimestamp, v.Expected.Headers.Timestamp)
	r.Header.Set(headerSessionID, v.Expected.Headers.SessionID)
	r.Header.Set(headerSaltID, v.Expected.Headers.SaltID)
	return r
}

func applyVectorMutations(req *http.Request, v parityVector) {
	switch v.Verifier {
	case "reject_missing_salt_id":
		req.Header.Del(headerSaltID)
	case "reject_tampered":
		req.Header.Set(headerAuth, flipFirstNibble(v.Expected.Headers.Authorization))
	}
}

// flipFirstNibble flips the low bit of the first hex character so the tag
// fails constant-time compare without changing length. Returns the input
// unchanged for empty strings.
func flipFirstNibble(tag string) string {
	if tag == "" {
		return tag
	}
	b := []byte(tag)
	switch b[0] {
	case '0':
		b[0] = '1'
	default:
		b[0] = '0'
	}
	return string(b)
}

func assertVectorOutcome(t *testing.T, v parityVector, err error) {
	t.Helper()
	switch v.Verifier {
	case "pass":
		if err != nil {
			t.Fatalf("expected verifier to accept vector %q, got error: %v", v.Name, err)
		}
	case "reject_missing_salt_id":
		if !errors.Is(err, ErrMissingSaltID) {
			t.Fatalf("expected ErrMissingSaltID for vector %q, got: %v", v.Name, err)
		}
	case "reject_timestamp", "reject_tampered":
		if err == nil {
			t.Fatalf("expected verifier to reject vector %q, got nil error", v.Name)
		}
		if errors.Is(err, ErrMissingSaltID) {
			t.Fatalf("vector %q rejected for the wrong reason (saltId): %v", v.Name, err)
		}
	default:
		t.Fatalf("unknown verifier discriminator %q for vector %q", v.Verifier, v.Name)
	}
}
