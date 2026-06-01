// Cross-language SDK parity test.
//
// Loads tests/conformance/scenarios.json and verifies that for each scenario
// the SDK (or raw HTTP, as a fallback) produces a response matching the
// declared assertion vector. Same JSON drives parametrized tests in every
// language SDK; drift between SDKs surfaces here.
//
// Requires an isa-mock server reachable at ISA_MOCK_URL (defaults to
// http://127.0.0.1:4010). When the mock is unreachable, every scenario is
// skipped so local `go test` doesn't fail on a developer machine without
// the mock running.

package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	scenarioDefaultMockURL = "http://127.0.0.1:4010"
	scenarioProbeTimeout   = 500 * time.Millisecond
	scenarioRequestTimeout = 5 * time.Second
	scenarioMinCount       = 10
	scenarioJSONNull       = "null"
)

type scenarioRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	BodyRaw *string           `json:"body_raw,omitempty"`
}

type scenarioExpected struct {
	Status               int      `json:"status"`
	ContentType          string   `json:"content_type,omitempty"`
	EnvelopeFields       []string `json:"envelope_fields,omitempty"`
	Code                 *string  `json:"code"`
	IdempotencyKeyEchoed bool     `json:"idempotency_key_echoed,omitempty"`
	ProblemFields        []string `json:"problem_fields,omitempty"`
}

type scenario struct {
	Name     string           `json:"name"`
	Request  scenarioRequest  `json:"request"`
	Expected scenarioExpected `json:"expected"`
}

type scenarioFile struct {
	Scenarios []scenario `json:"scenarios"`
}

func loadConformanceScenarios(t *testing.T) []scenario {
	t.Helper()
	path := scenariosPath(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("conformance: read %s: %v", path, err)
	}
	var sf scenarioFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatalf("conformance: parse %s: %v", path, err)
	}
	if len(sf.Scenarios) < scenarioMinCount {
		t.Fatalf("conformance: expected >=%d scenarios, got %d", scenarioMinCount, len(sf.Scenarios))
	}
	return sf.Scenarios
}

func scenariosPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("conformance: getwd: %v", err)
	}
	return filepath.Join(wd, "..", "..", "tests", "conformance", "scenarios.json")
}

func mockReachable(mockURL string) bool {
	client := &http.Client{Timeout: scenarioProbeTimeout}
	resp, err := client.Get(mockURL + "/__healthz_probe__")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

func TestConformanceScenarios_FileLoadsAndHasMinimumCases(t *testing.T) {
	scenarios := loadConformanceScenarios(t)
	if len(scenarios) < scenarioMinCount {
		t.Fatalf("expected >=%d scenarios, got %d", scenarioMinCount, len(scenarios))
	}
}

func TestConformanceScenarios_AgainstIsaMock(t *testing.T) {
	mockURL, hasMockURL := os.LookupEnv("ISA_MOCK_URL")
	if !hasMockURL || mockURL == "" {
		mockURL = scenarioDefaultMockURL
	}
	if !mockReachable(mockURL) {
		if hasMockURL {
			t.Fatalf("isa-mock unreachable at %s", mockURL)
		}
		t.Skipf("isa-mock unreachable at %s; skipping", mockURL)
	}
	for _, s := range loadConformanceScenarios(t) {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			runScenario(t, mockURL, s)
		})
	}
}

func runScenario(t *testing.T, mockURL string, s scenario) {
	t.Helper()
	var body io.Reader
	if s.Request.BodyRaw != nil {
		body = strings.NewReader(*s.Request.BodyRaw)
	} else if len(s.Request.Body) > 0 && string(s.Request.Body) != scenarioJSONNull {
		body = bytes.NewReader(s.Request.Body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), scenarioRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, s.Request.Method, mockURL+s.Request.Path, body)
	if err != nil {
		t.Fatalf("scenario %s: new request: %v", s.Name, err)
	}
	for k, v := range s.Request.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scenario %s: do request: %v", s.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != s.Expected.Status {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("scenario %s: status mismatch — want %d, got %d, body=%q",
			s.Name, s.Expected.Status, resp.StatusCode, string(respBody))
	}

	assertScenarioBody(t, s, resp)
}

func assertScenarioBody(t *testing.T, s scenario, resp *http.Response) {
	t.Helper()
	ct := resp.Header.Get("Content-Type")
	if s.Expected.ContentType != "" && !strings.Contains(ct, s.Expected.ContentType) {
		t.Fatalf("scenario %s: content-type missing %q; got %q", s.Name, s.Expected.ContentType, ct)
	}
	if !strings.Contains(ct, "json") {
		return
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("scenario %s: read body: %v", s.Name, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("scenario %s: parse body %q: %v", s.Name, string(raw), err)
	}
	for _, field := range s.Expected.EnvelopeFields {
		if _, ok := payload[field]; !ok {
			t.Errorf("scenario %s: envelope missing %q (payload=%v)", s.Name, field, payload)
		}
	}
	for _, field := range s.Expected.ProblemFields {
		if _, ok := payload[field]; !ok {
			t.Errorf("scenario %s: ProblemDetails missing %q (payload=%v)", s.Name, field, payload)
		}
	}
	if s.Expected.Code != nil {
		if payload["code"] != *s.Expected.Code {
			t.Errorf("scenario %s: code mismatch — want %q, got %v", s.Name, *s.Expected.Code, payload["code"])
		}
	}
	if s.Expected.IdempotencyKeyEchoed {
		sent := s.Request.Headers["X-Isa-Idempotency-Key"]
		if sent == "" {
			t.Errorf("scenario %s: request missing idempotency key", s.Name)
		}
		if payload["idempotency_key"] != sent {
			t.Errorf("scenario %s: envelope idempotency_key %v did not echo request key %q",
				s.Name, payload["idempotency_key"], sent)
		}
	}
}
