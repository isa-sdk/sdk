package algosure

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParseAlgosureRequest_MissingSaltID_ReturnsErrMissingSaltID(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", strings.NewReader(""))
	r.Header.Set(headerHost, "h.example")
	r.Header.Set(headerTimestamp, "1")
	r.Header.Set(headerSessionID, "sid")
	r.Header.Set(headerAuth, "tag")

	_, err := parseAlgosureRequest(r)
	if !errors.Is(err, ErrMissingSaltID) {
		t.Fatalf("parseAlgosureRequest() err = %v; want ErrMissingSaltID", err)
	}
}

func TestParseAlgosureRequest_InvalidSaltID_ReturnsErrMissingSaltID(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", strings.NewReader(""))
	r.Header.Set(headerHost, "h.example")
	r.Header.Set(headerTimestamp, "1")
	r.Header.Set(headerSessionID, "sid")
	r.Header.Set(headerAuth, "tag")
	r.Header.Set(headerSaltID, "not-a-number")

	_, err := parseAlgosureRequest(r)
	if !errors.Is(err, ErrMissingSaltID) {
		t.Fatalf("parseAlgosureRequest() err = %v; want ErrMissingSaltID", err)
	}
}

func TestParseAlgosureRequest_NonPositiveSaltID_ReturnsErrMissingSaltID(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", strings.NewReader(""))
	r.Header.Set(headerHost, "h.example")
	r.Header.Set(headerTimestamp, "1")
	r.Header.Set(headerSessionID, "sid")
	r.Header.Set(headerAuth, "tag")
	r.Header.Set(headerSaltID, "0")

	_, err := parseAlgosureRequest(r)
	if !errors.Is(err, ErrMissingSaltID) {
		t.Fatalf("parseAlgosureRequest() err = %v; want ErrMissingSaltID", err)
	}
}

func TestParseAlgosureRequest_LowercaseSaltIdHeaderAccepted(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", strings.NewReader(""))
	r.Header.Set(headerHost, "h.example")
	r.Header.Set(headerTimestamp, "1")
	r.Header.Set(headerSessionID, "sid")
	r.Header.Set(headerAuth, "tag")
	r.Header.Set("*saltid", strconv.FormatInt(99, 10))

	in, err := parseAlgosureRequest(r)
	if err != nil {
		t.Fatalf("parseAlgosureRequest: %v", err)
	}
	if in.saltID != 99 {
		t.Fatalf("saltID = %d; want 99", in.saltID)
	}
}

func TestParseAlgosureRequest_ValidSaltID_Parsed(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", strings.NewReader(""))
	r.Header.Set(headerHost, "h.example")
	r.Header.Set(headerTimestamp, "1")
	r.Header.Set(headerSessionID, "sid")
	r.Header.Set(headerAuth, "tag")
	r.Header.Set(headerSaltID, "42")

	in, err := parseAlgosureRequest(r)
	if err != nil {
		t.Fatalf("parseAlgosureRequest: %v", err)
	}
	if in.saltID != 42 {
		t.Fatalf("saltID = %d; want 42", in.saltID)
	}
}
