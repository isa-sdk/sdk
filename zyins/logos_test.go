package zyins

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type logoFakeDoer struct {
	last *http.Request
	resp *http.Response
}

func (f *logoFakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.last = req
	return f.resp, nil
}

func newLogoClient(t *testing.T, body string, status int, ct string) (*Client, *logoFakeDoer) {
	t.Helper()
	doer := &logoFakeDoer{resp: &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{ct}},
	}}
	c, err := NewClient(WithToken("isa_test_abc"), WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.doer = doer
	return c, doer
}

func TestLogos_Get_RawBytes(t *testing.T) {
	c, doer := newLogoClient(t, "PNGDATA", 200, "image/png")
	body, dataURI, err := c.Logos.Get(context.Background(), "aetna")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if dataURI != "" {
		t.Errorf("dataURI should be empty for raw call, got %q", dataURI)
	}
	if string(body) != "PNGDATA" {
		t.Errorf("body=%q", body)
	}
	if doer.last.URL.Path != "/v1/logo/aetna" {
		t.Errorf("path=%q", doer.last.URL.Path)
	}
	if doer.last.URL.RawQuery != "" {
		t.Errorf("expected no query for raw, got %q", doer.last.URL.RawQuery)
	}
}

func TestLogos_Get_DataURI(t *testing.T) {
	c, doer := newLogoClient(t, "data:image/png;base64,AAAA", 200, "text/plain")
	body, dataURI, err := c.Logos.Get(context.Background(), "aetna", WithDataURI(true))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if body != nil {
		t.Errorf("body should be nil for data-URI, got %v", body)
	}
	if !strings.HasPrefix(dataURI, "data:image/png;base64,") {
		t.Errorf("dataURI=%q", dataURI)
	}
	if doer.last.URL.RawQuery != "ds=true" {
		t.Errorf("expected ds=true, got %q", doer.last.URL.RawQuery)
	}
}

func TestLogos_Get_RejectsEmptyCarrier(t *testing.T) {
	c, _ := newLogoClient(t, "", 200, "")
	if _, _, err := c.Logos.Get(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty carrier")
	}
}

func TestLogos_Get_NonImageDataURIRejected(t *testing.T) {
	c, _ := newLogoClient(t, "not a data uri", 200, "text/plain")
	if _, _, err := c.Logos.Get(context.Background(), "aetna", WithDataURI(true)); err == nil {
		t.Fatal("expected error for non-data-URI body")
	}
}
