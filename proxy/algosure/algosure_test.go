package algosure

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isa-sdk/sdk/core/replay"
)

const (
	testHost      = "example.agent.com"
	testAccountID = "acct_123"
	testSessionID = "sess_abc"
	testSalt      = "the-quick-brown-fox-jumps-over-the-lazy-dog-0123456789abcdef"
	testPath      = "/v1/ping"
	testSaltID    = int64(42)
)

type staticHosts struct{ accountID string }

func (s *staticHosts) LookupHost(_ context.Context, _ string) (string, error) {
	return s.accountID, nil
}

// staticSalt returns testSalt for testSaltID. For any other id it returns
// otherSalt when set (so wrong-id cases see a distinct byte sequence); when
// otherSalt is nil it still returns the primary salt so callers that only
// need a successful fetch for a non-matching id (without asserting tag
// divergence) keep working.
type staticSalt struct {
	salt      []byte
	otherSalt []byte
}

func (s *staticSalt) FetchByID(_ context.Context, saltID int64, _ string) ([]byte, error) {
	if saltID == testSaltID {
		return s.salt, nil
	}
	if s.otherSalt != nil {
		return s.otherSalt, nil
	}
	return s.salt, nil
}

type erroringCache struct{ err error }

func (e *erroringCache) SeenOnce(_ context.Context, _ string) (bool, error) {
	return false, e.err
}

func newTestVerifier(t *testing.T, cache replay.Cache, now func() time.Time) *Verifier {
	t.Helper()
	v, err := NewVerifier(Config{
		Hosts:  &staticHosts{accountID: testAccountID},
		Salt:   &staticSalt{salt: []byte(testSalt)},
		Replay: cache,
		Now:    now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func signAlgosureRequest(tsMillis int64, salt []byte) *http.Request {
	const body = ""
	r := httptest.NewRequest(http.MethodGet, testPath, strings.NewReader(body))
	tsStr := strconv.FormatInt(tsMillis, 10)

	var keyBuf [maxSimpleKeyLen]byte
	simpleKey := deriveSimpleKey(&keyBuf, salt, tsMillis)

	bodyHash := sha256.Sum256([]byte(body))
	message := strings.Join([]string{
		http.MethodGet,
		testPath,
		hex.EncodeToString(bodyHash[:]),
		tsStr,
		testSessionID,
	}, "\x00")
	mac := hmac.New(sha256.New, simpleKey)
	mac.Write([]byte(message))
	tag := hex.EncodeToString(mac.Sum(nil))

	r.Header.Set(headerHost, testHost)
	r.Header.Set(headerTimestamp, tsStr)
	r.Header.Set(headerSessionID, testSessionID)
	r.Header.Set(headerSaltID, strconv.FormatInt(testSaltID, 10))
	r.Header.Set(headerAuth, tag)
	return r
}

func TestNewVerifier_RequiresReplay(t *testing.T) {
	_, err := NewVerifier(Config{
		Hosts: &staticHosts{accountID: testAccountID},
		Salt:  &staticSalt{salt: []byte(testSalt)},
	})
	if err == nil {
		t.Fatal("expected nil-Replay error; got nil")
	}
}

func TestVerify_FirstUseSucceeds(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cache, _ := replay.NewInMemoryCache(replay.MemoryConfig{Window: 60 * time.Second})
	v := newTestVerifier(t, cache, func() time.Time { return now })

	r := signAlgosureRequest(now.UnixMilli(), []byte(testSalt))
	ctx, err := v.Verify(context.Background(), r)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if ctx.AccountID != testAccountID {
		t.Fatalf("accountID=%q; want %q", ctx.AccountID, testAccountID)
	}
}

func TestVerify_SecondUseReturnsErrReplay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cache, _ := replay.NewInMemoryCache(replay.MemoryConfig{Window: 60 * time.Second})
	v := newTestVerifier(t, cache, func() time.Time { return now })

	r1 := signAlgosureRequest(now.UnixMilli(), []byte(testSalt))
	if _, err := v.Verify(context.Background(), r1); err != nil {
		t.Fatalf("first verify failed: %v", err)
	}

	r2 := signAlgosureRequest(now.UnixMilli(), []byte(testSalt))
	_, err := v.Verify(context.Background(), r2)
	if !errors.Is(err, ErrReplay) {
		t.Fatalf("second verify returned %v; want ErrReplay", err)
	}
}

func TestVerify_AfterWindowExpiryReverifies(t *testing.T) {
	clockNow := time.Unix(1_700_000_000, 0)
	window := 60 * time.Second
	cache, _ := replay.NewInMemoryCache(replay.MemoryConfig{
		Window: window,
		Now:    func() time.Time { return clockNow },
	})
	v := newTestVerifier(t, cache, func() time.Time { return clockNow })

	r1 := signAlgosureRequest(clockNow.UnixMilli(), []byte(testSalt))
	if _, err := v.Verify(context.Background(), r1); err != nil {
		t.Fatalf("first verify failed: %v", err)
	}

	// Advance past window. Timestamp check would reject a replay of the old
	// tag here anyway; this asserts a fresh tag at a new timestamp works.
	clockNow = clockNow.Add(2 * window)
	r2 := signAlgosureRequest(clockNow.UnixMilli(), []byte(testSalt))
	if _, err := v.Verify(context.Background(), r2); err != nil {
		t.Fatalf("post-window verify failed: %v", err)
	}
}

func TestVerify_CacheErrorFailsClosed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	v := newTestVerifier(t, &erroringCache{err: errors.New("redis down")}, func() time.Time { return now })

	r := signAlgosureRequest(now.UnixMilli(), []byte(testSalt))
	if _, err := v.Verify(context.Background(), r); err == nil {
		t.Fatal("expected fail-closed error; got nil")
	}
}

func TestVerify_RejectsTimestampOutsideTolerance(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cache, _ := replay.NewInMemoryCache(replay.MemoryConfig{Window: 60 * time.Second})
	v := newTestVerifier(t, cache, func() time.Time { return now })

	// Client timestamp 5 minutes stale — beyond default 30s tolerance.
	stale := now.Add(-5 * time.Minute).UnixMilli()
	r := signAlgosureRequest(stale, []byte(testSalt))
	if _, err := v.Verify(context.Background(), r); err == nil {
		t.Fatal("expected timestamp drift error; got nil")
	}
}
