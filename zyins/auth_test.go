package zyins

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestStaticToken_Token_ReturnsValue(t *testing.T) {
	tok, err := StaticToken("isa_live_abc").Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "isa_live_abc" {
		t.Fatalf("got %q, want isa_live_abc", tok)
	}
}

func TestStaticToken_Token_EmptyReturnsError(t *testing.T) {
	_, err := StaticToken("   ").Token()
	if err == nil {
		t.Fatalf("expected error for empty token")
	}
}

func TestValidateTokenShape_AcceptsLiveAndTest(t *testing.T) {
	for _, tok := range []string{"isa_live_abc", "isa_test_xyz"} {
		if err := validateTokenShape(tok); err != nil {
			t.Errorf("validateTokenShape(%q) returned %v; want nil", tok, err)
		}
	}
}

func TestValidateTokenShape_RejectsLegacyAndEmpty(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"whitespace_only":    "   ",
		"legacy":             "zyins_live_abc",
		"unprefixed":         "abc123",
		"wrong_brand":        "stripe_test_abc",
		"leading_whitespace": "  isa_live_abc",
		"trailing_newline":   "isa_live_abc\n",
		"trailing_space":     "isa_test_abc ",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateTokenShape(tok)
			if err == nil {
				t.Fatalf("expected error for %q", tok)
			}
		})
	}
}

func TestStaticToken_RejectsSurroundingWhitespace(t *testing.T) {
	cases := []string{" isa_live_abc", "isa_live_abc ", "\tisa_test_abc", "isa_test_abc\n"}
	for _, tok := range cases {
		t.Run(fmt.Sprintf("%q", tok), func(t *testing.T) {
			_, err := StaticToken(tok).Token()
			if err == nil {
				t.Fatalf("expected error for token with surrounding whitespace: %q", tok)
			}
		})
	}
}

func TestNewClient_RequiresToken(t *testing.T) {
	_, err := NewClient()
	if err == nil {
		t.Fatalf("expected error without WithToken")
	}
	if !strings.Contains(err.Error(), "WithToken") {
		t.Fatalf("unexpected message: %v", err)
	}
}

type refreshingTokenSource struct {
	calls int
	err   error
}

func (r *refreshingTokenSource) Token() (string, error) {
	r.calls++
	if r.err != nil {
		return "", r.err
	}
	return "isa_live_refreshed", nil
}

func TestWithTokenSource_NilReturnsError(t *testing.T) {
	_, err := NewClient(WithTokenSource(nil))
	if err == nil {
		t.Fatalf("expected error for nil TokenSource")
	}
}

func TestWithTokenSource_CustomImpl(t *testing.T) {
	src := &refreshingTokenSource{}
	_, err := NewClient(WithTokenSource(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTokenSourceAdapter_PropagatesError(t *testing.T) {
	want := errors.New("rotation failed")
	src := &refreshingTokenSource{err: want}
	adapter := asCoreTokenSource(src)
	_, err := adapter.Token()
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped %v; got %v", want, err)
	}
}
