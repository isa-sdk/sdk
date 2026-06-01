package zyins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// captureLogger records every Debug/Warn call so tests can assert
// redaction behavior without parsing slog output.
type captureLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

type logEntry struct {
	level string
	msg   string
	args  []any
}

func (c *captureLogger) Debug(msg string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, logEntry{level: "debug", msg: msg, args: args})
}

func (c *captureLogger) Warn(msg string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, logEntry{level: "warn", msg: msg, args: args})
}

// find returns the first entry matching msg, or nil.
func (c *captureLogger) find(msg string) *logEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.entries {
		if c.entries[i].msg == msg {
			return &c.entries[i]
		}
	}
	return nil
}

// argMap reshapes the variadic slog args (key, value, key, value, …)
// into a map for ergonomic assertions.
func argMap(entry *logEntry) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(entry.args); i += 2 {
		if k, ok := entry.args[i].(string); ok {
			out[k] = entry.args[i+1]
		}
	}
	return out
}

func TestDebugLogger_RedactsAuthorizationHeader(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"period_start":"2026-05-01","period_end":"2026-05-31","prequalify_count":1,"quote_count":0,"quota_limit":100,"request_id":"req_x"}}`))
	}))
	defer srv.Close()

	logger := &captureLogger{}
	c, err := NewClient(
		WithToken("isa_test_4fjK2nQ7mX1aB8sR9pZ3"),
		WithBaseURL(srv.URL),
		WithMaxRetryAttempts(1),
		WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Usage.Current(context.Background()); err != nil {
		t.Fatalf("Usage.Current: %v", err)
	}
	req := logger.find("zyins.request")
	if req == nil {
		t.Fatalf("expected zyins.request log entry; got %+v", logger.entries)
	}
	hdrs, ok := argMap(req)["headers"].(map[string]string)
	if !ok {
		t.Fatalf("headers arg shape: %T", argMap(req)["headers"])
	}
	if hdrs["Authorization"] != redactedPlaceholder {
		t.Errorf("Authorization not redacted: %q", hdrs["Authorization"])
	}
}

func TestDebugLogger_RedactsPIIBodyFields(t *testing.T) {
	t.Parallel()
	body := `{"applicant":{"email":"john.doe@acme-agency.com","dob":"1962-04-18","phone":"555-1212","ssn":"123-45-6789","state":"NC"}}`
	out := redactBody([]byte(body))
	for _, field := range []string{"john.doe", "1962-04-18", "555-1212", "123-45-6789"} {
		if strings.Contains(out, field) {
			t.Errorf("redacted body still contains %q: %s", field, out)
		}
	}
	if !strings.Contains(out, redactedPlaceholder) {
		t.Errorf("expected redaction placeholder in output: %s", out)
	}
	if !strings.Contains(out, `"state":"NC"`) {
		t.Errorf("non-PII field state should survive redaction: %s", out)
	}
}

func TestRedactBody_NonJSONFallsThrough(t *testing.T) {
	t.Parallel()
	out := redactBody([]byte("plain text"))
	if out != "plain text" {
		t.Errorf("non-JSON body should pass through unchanged; got %q", out)
	}
}

func TestRedactBody_EmptyReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := redactBody(nil); got != "" {
		t.Errorf("nil body should be empty; got %q", got)
	}
}

func TestSanitizeLogField_StripsLineBreaks(t *testing.T) {
	t.Parallel()
	got := sanitizeLogField("line1\nline2\rline3")
	if strings.Contains(got, "\n") || strings.Contains(got, "\r") {
		t.Errorf("sanitizeLogField must remove line breaks; got %q", got)
	}
	if got != "line1line2line3" {
		t.Errorf("unexpected sanitized value: %q", got)
	}
}

func TestSafeURLForLog_StripsQueryAndUserinfo(t *testing.T) {
	t.Parallel()
	raw, err := url.Parse("https://user:secret@api.example.com/v1/prequalify?token=abc&state=TX")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	got := safeURLForLog(raw)
	if strings.Contains(got, "secret") || strings.Contains(got, "token=abc") {
		t.Errorf("log URL must not include credentials or query; got %q", got)
	}
	if !strings.Contains(got, "api.example.com/v1/prequalify") {
		t.Errorf("log URL should retain host and path; got %q", got)
	}
}
