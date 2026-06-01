// Debug logging: when ISA_LOG=debug, request/response bodies and
// headers are dumped to stderr via slog with a stderr-only handler.
//
// stderr — never stdout — because parent/child JSON pipelines route
// stdout to the next process; co-mingling debug noise with structured
// output is the well-known Anthropic SDK bug we will not reproduce
// (SDK_DESIGN.md §7.1).

package zyins

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// EnvLogVar is the env var consulted to enable debug logging. Setting
// it to "debug" emits request/response trails on stderr. Any other
// value (including unset) disables debug output.
const EnvLogVar = "ISA_LOG"

// debugLogValue is the literal that turns debug logging on.
const debugLogValue = "debug"

// maxBodyDump caps the number of body bytes echoed to the logger.
// Larger bodies are truncated with an explicit suffix so the operator
// is never surprised by the elision.
const maxBodyDump = 4 << 10 // 4 KiB

// redactedPlaceholder replaces sensitive header values and PII body
// fields in the debug stream. The literal is documented so reviewers
// can grep for accidental leakage.
const redactedPlaceholder = "[REDACTED]"

// redactedHeaders enumerates the header names whose values must never
// appear in the debug stream. Lookup is case-insensitive on the HTTP
// canonical form. The set is small and stable; if it changes, audit
// callers for new transport modes.
var redactedHeaders = map[string]struct{}{
	"Authorization":       {},
	"X-Device-Signature":  {},
	"X-Session-Signature": {},
	"Idempotency-Key":     {}, // surface separately on the structured log key
}

// redactedBodyFields enumerates the JSON body keys whose values must
// never appear in the debug stream. PII per SDK_DESIGN.md §7.1.
var redactedBodyFields = map[string]struct{}{
	"email": {},
	"dob":   {},
	"ssn":   {},
	"phone": {},
}

// DebugLogger is the minimal interface the SDK depends on. slog.Logger
// satisfies it; alternative loggers (zap, zerolog adapter) plug in via
// WithLogger.
//
// Two methods rather than the full slog surface so a custom adapter
// stays small. Debug is the chatty channel (per-request); Warn is the
// recoverable-but-notable channel (e.g., retry exhaustion).
type DebugLogger interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
}

// newDefaultDebugLogger returns a slog.Logger whose handler writes to
// stderr at debug level when ISA_LOG=debug, otherwise at warn level.
// The handler is text-formatted; structured callers should pass their
// own slog.Logger via WithLogger.
func newDefaultDebugLogger() DebugLogger {
	level := slog.LevelWarn
	if strings.EqualFold(os.Getenv(EnvLogVar), debugLogValue) {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

// debugDoer wraps an inner httpDoer and emits redacted request/response
// trails to the logger. The wrapping is invisible to callers — the
// public surface still sees an httpDoer.
type debugDoer struct {
	inner  httpDoer
	logger DebugLogger
}

// Do logs the request, forwards to the inner doer, logs the response,
// and returns both. Logging never alters response semantics: the body
// is buffered and replaced with a fresh io.NopCloser so downstream
// readers see identical bytes.
func (d *debugDoer) Do(req *http.Request) (*http.Response, error) {
	if d.logger != nil {
		d.logger.Debug("zyins.request",
			"method", sanitizeLogField(req.Method),
			"url", safeURLForLog(req.URL),
			"headers", redactHeaders(req.Header),
			"body", snapshotRequestBody(req),
		)
	}
	resp, err := d.inner.Do(req)
	if err != nil {
		if d.logger != nil {
			d.logger.Debug("zyins.response.error", "err", sanitizeLogField(err.Error()))
		}
		return nil, err
	}
	if d.logger != nil {
		bodyBytes, replaced := snapshotResponseBody(resp)
		d.logger.Debug("zyins.response",
			"status", resp.StatusCode,
			"headers", redactHeaders(resp.Header),
			"body", redactBody(bodyBytes),
			"request_id", sanitizeLogField(resp.Header.Get("X-Request-Id")),
		)
		if replaced != nil {
			resp.Body = replaced
		}
	}
	return resp, nil
}

// sanitizeLogField strips line breaks from user-influenced values before
// they are written to logs, preventing forged log entries (CWE-117).
func sanitizeLogField(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	return strings.ReplaceAll(s, "\r", "")
}

// safeURLForLog returns a log-safe URL with credentials, query, and
// fragment removed so user-controlled query values cannot reach logs.
func safeURLForLog(u *url.URL) string {
	if u == nil {
		return ""
	}
	scrubbed := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	return scrubbed.Redacted()
}

// redactHeaders returns a flat key=value slice safe to ship into the
// logger. Sensitive header values are masked; everything else is
// joined with a comma for stable single-line output.
func redactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if _, sensitive := redactedHeaders[http.CanonicalHeaderKey(k)]; sensitive {
			out[k] = redactedPlaceholder
			continue
		}
		out[k] = sanitizeLogField(strings.Join(v, ","))
	}
	return out
}

// snapshotRequestBody reads the entire request body, replaces it with
// a fresh reader so the round-trip is unaffected, and returns the
// redacted contents. Empty bodies return an empty string.
func snapshotRequestBody(req *http.Request) string {
	if req.Body == nil {
		return ""
	}
	buf, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return sanitizeLogField("<failed to read request body: " + err.Error() + ">")
	}
	req.Body = io.NopCloser(bytes.NewReader(buf))
	return redactBody(buf)
}

// snapshotResponseBody mirrors snapshotRequestBody for responses,
// returning the bytes and a replacement ReadCloser the caller MAY
// install on resp.Body so downstream consumers see identical content.
func snapshotResponseBody(resp *http.Response) ([]byte, io.ReadCloser) {
	if resp.Body == nil {
		return nil, nil
	}
	buf, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return []byte(sanitizeLogField("<failed to read response body: " + err.Error() + ">")), nil
	}
	return buf, io.NopCloser(bytes.NewReader(buf))
}

// redactBody returns a JSON-safe representation of body with PII fields
// masked. Non-JSON bodies are truncated unchanged (no PII pattern is
// known to live in non-JSON request shapes today; the consideration is
// listed in SDK_DESIGN.md as a future hardening item).
func redactBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if !looksLikeJSON(body) {
		return sanitizeLogField(truncate(string(body)))
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return sanitizeLogField(truncate(string(body)))
	}
	redacted := redactJSONValue(raw)
	out, err := json.Marshal(redacted)
	if err != nil {
		return sanitizeLogField(truncate(string(body)))
	}
	return sanitizeLogField(truncate(string(out)))
}

// redactJSONValue walks a parsed JSON value and replaces sensitive
// field values with the placeholder. Recurses through objects and
// arrays so nested PII is caught.
func redactJSONValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, val := range typed {
			if _, sensitive := redactedBodyFields[strings.ToLower(k)]; sensitive {
				out[k] = redactedPlaceholder
				continue
			}
			out[k] = redactJSONValue(val)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, val := range typed {
			out[i] = redactJSONValue(val)
		}
		return out
	default:
		return v
	}
}

// looksLikeJSON returns true when body begins with `{` or `[` after
// leading whitespace. Cheap heuristic; mis-detections fall through to
// the truncate-only path which is still safe.
func looksLikeJSON(body []byte) bool {
	for _, b := range body {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[':
			return true
		default:
			return false
		}
	}
	return false
}

// truncate caps the dumped string at maxBodyDump bytes so a verbose
// payload (e.g., a paginated dataset) does not overflow the logger.
func truncate(s string) string {
	if len(s) <= maxBodyDump {
		return s
	}
	return s[:maxBodyDump] + "...<truncated>"
}
