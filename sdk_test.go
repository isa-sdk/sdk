package sdk

import (
	"errors"
	"strings"
	"testing"

	"github.com/isa-sdk/sdk/zyins"
)

func TestWithLicense_PendingTransportReturnsConfigError(t *testing.T) {
	t.Parallel()
	isa, err := WithLicense(LicenseOptions{
		Keycode: "ABC-123-XYZ",
		Email:   "agent@example.com",
	})
	if isa != nil {
		t.Fatal("expected nil Isa while License transport is pending")
	}
	var ce *zyins.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *zyins.ConfigError; got %T: %v", err, err)
	}
	if !strings.Contains(ce.Error(), "License auth mode") {
		t.Errorf("error must explain pending transport; got %q", ce.Error())
	}
}

func TestWithSession_PendingTransportReturnsConfigError(t *testing.T) {
	t.Parallel()
	isa, err := WithSession(SessionOptions{
		SessionID:     "sess_abc",
		SessionSecret: "secret_xyz",
	})
	if isa != nil {
		t.Fatal("expected nil Isa while Session transport is pending")
	}
	var ce *zyins.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *zyins.ConfigError; got %T: %v", err, err)
	}
	if !strings.Contains(ce.Error(), "Session auth mode") {
		t.Errorf("error must explain pending transport; got %q", ce.Error())
	}
}

func TestWithLicense_MissingEnvReturnsConfigError(t *testing.T) {
	t.Parallel()
	isa, err := WithLicense(LicenseOptions{})
	if isa != nil {
		t.Fatal("expected nil Isa")
	}
	var ce *zyins.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *zyins.ConfigError; got %T: %v", err, err)
	}
	if ce.Factory != "WithLicense" {
		t.Errorf("Factory = %q, want WithLicense", ce.Factory)
	}
}
