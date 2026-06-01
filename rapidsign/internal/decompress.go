package internal

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
)

// DecodeGzippedBase64 inverts the wire format used by
// DownloadDocument: base64 → gzip → raw PDF bytes. Returns a typed
// error when either decode step fails so callers can distinguish
// transport-level corruption from server-level errors (which are
// classified before reaching this helper).
func DecodeGzippedBase64(s string) ([]byte, error) {
	gz, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("rapidsign: failed to base64-decode download payload: %w", err)
	}
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("rapidsign: failed to open gzip reader on download payload: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("rapidsign: failed to read gzip-decompressed download payload: %w", err)
	}
	return out, nil
}
