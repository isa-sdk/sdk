package rapidsign

import (
	"net/http"
	"strings"
	"testing"
)

func TestNew_RejectsEmptyToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"tab", "\t"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tc.token); err == nil {
				t.Fatalf("expected non-nil error for token %q", tc.token)
			}
		})
	}
}

func TestNew_DefaultsBaseURLAndServices(t *testing.T) {
	t.Parallel()
	c, err := New("isa_test_abc")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.Documents == nil || c.Webhooks == nil {
		t.Fatal("Documents and Webhooks must be non-nil after New")
	}
	if !strings.HasPrefix(c.userAgent, "isa-sdk-go-rapidsign/") {
		t.Fatalf("userAgent default = %q, want isa-sdk-go-rapidsign/ prefix", c.userAgent)
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	c, err := New("isa_test_abc", Options{BaseURL: "https://staging.example.com/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != "https://staging.example.com" {
		t.Fatalf("baseURL = %q, want trailing slash removed", c.baseURL)
	}
}

func TestNew_WhitespaceBaseURLUsesDefault(t *testing.T) {
	t.Parallel()
	c, err := New("isa_test_abc", Options{BaseURL: "   "})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
}

func TestNew_InvalidBaseURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		baseURL string
	}{
		{"missing-scheme", "staging.example.com"},
		{"invalid-scheme", "ftp://example.com"},
		{"missing-host", "https://"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New("isa_test_abc", Options{BaseURL: tc.baseURL}); err == nil {
				t.Fatalf("expected error for base URL %q", tc.baseURL)
			}
		})
	}
}

func TestNew_RejectsMultipleOptions(t *testing.T) {
	t.Parallel()
	if _, err := New("isa_test_abc", Options{UserAgent: "a"}, Options{UserAgent: "b"}); err == nil {
		t.Fatal("expected error when multiple Options are passed")
	}
}

func TestNew_CustomHTTPClientWithoutToken(t *testing.T) {
	t.Parallel()
	c, err := New("", Options{HTTPClient: &http.Client{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatal("expected client")
	}
}

func TestNew_UserAgentOverride(t *testing.T) {
	t.Parallel()
	c, err := New("isa_test_abc", Options{UserAgent: "my-app/1.0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.userAgent != "my-app/1.0" {
		t.Fatalf("userAgent = %q, want my-app/1.0", c.userAgent)
	}
}
