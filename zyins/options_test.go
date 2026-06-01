package zyins

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stubServer returns an httptest.Server that responds 200 with body to
// every request. Callers must close it.
func stubServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func TestOptions_Rejections(t *testing.T) {
	cases := map[string]Option{
		"WithBaseURL_blank":             WithBaseURL("   "),
		"WithHTTPClient_nil":            WithHTTPClient(nil),
		"WithTimeout_zero":              WithTimeout(0),
		"WithUserAgent_blank":           WithUserAgent("  "),
		"WithMaxRetryAttempts_zero":     WithMaxRetryAttempts(0),
		"WithMaxRetryAttempts_negative": WithMaxRetryAttempts(-3),
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewClient(WithToken("isa_test_x"), opt)
			if err == nil {
				t.Fatalf("expected error from %s", name)
			}
		})
	}
}

func TestOptions_Accept(t *testing.T) {
	c, err := NewClient(
		WithToken("isa_test_x"),
		WithBaseURL("https://example.com/"),
		WithHTTPClient(&http.Client{Timeout: time.Second}),
		WithTimeout(time.Second),
		WithUserAgent("custom/1.0"),
		WithMaxRetryAttempts(2),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.baseURL != "https://example.com" {
		t.Errorf("baseURL = %q (trailing slash should be trimmed)", c.baseURL)
	}
	if c.userAgent != "custom/1.0" {
		t.Errorf("userAgent = %q", c.userAgent)
	}
}

func TestNewHeightInches(t *testing.T) {
	h, err := NewHeightInches(70)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.TotalInches != 70 {
		t.Errorf("TotalInches = %d", h.TotalInches)
	}
	if _, err := NewHeightInches(-1); err == nil {
		t.Errorf("expected error for negative")
	}
}

func TestDatasets_NullDataEnvelopeReturnsError(t *testing.T) {
	// When the server emits `{"data": null}`, the SDK must surface an
	// explicit error rather than silently producing a zero-value page —
	// the caller should know the API returned no envelope, not assume
	// the result was an empty list.
	srv := stubServer(`{"data": null}`)
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.Datasets.Conditions(t.Context(), DatasetListOptions{}); err == nil {
		t.Fatalf("expected error for null data envelope")
	}
}

func TestDatasets_OtherEntities(t *testing.T) {
	// Each helper hits a different path; we exercise listDataset's
	// shared code through one call per helper to catch routing bugs.
	type call struct {
		body string
		fn   func(*Client) error
	}
	datasetBody := `{"data":{"data":[],"has_more":false}}`
	refBody := `{"data":[]}`
	calls := []call{
		{datasetBody, func(c *Client) error {
			_, err := c.Datasets.Medications(t.Context(), DatasetListOptions{})
			return err
		}},
		{datasetBody, func(c *Client) error {
			_, err := c.Datasets.Brands(t.Context(), DatasetListOptions{})
			return err
		}},
		{datasetBody, func(c *Client) error {
			_, err := c.Datasets.Plans(t.Context(), DatasetListOptions{})
			return err
		}},
		{refBody, func(c *Client) error {
			_, err := c.ReferenceData.ProductTypes(t.Context())
			return err
		}},
		{refBody, func(c *Client) error {
			_, err := c.ReferenceData.NicotineModes(t.Context())
			return err
		}},
	}
	for _, tc := range calls {
		srv := stubServer(tc.body)
		c := newTestClient(t, srv)
		if err := tc.fn(c); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		srv.Close()
	}
}
