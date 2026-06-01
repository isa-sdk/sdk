package zyins

import (
	"errors"
	"strings"
	"testing"
)

// mapEnv is a test envReader backed by a literal map. Used instead of
// os.Setenv to keep the test suite parallel-safe.
type mapEnv map[string]string

func (m mapEnv) get(key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func TestWithBearer_ReadsISA_TOKEN(t *testing.T) {
	t.Parallel()
	env := mapEnv{EnvTokenVar: "isa_test_4fjK2nQ7mX1aB8sR9pZ3"}
	opt, err := withBearerFrom(env)
	if err != nil {
		t.Fatalf("withBearerFrom: %v", err)
	}
	if opt == nil {
		t.Fatalf("expected non-nil Option")
	}
	if _, err := NewClient(opt); err != nil {
		t.Fatalf("NewClient: %v", err)
	}
}

func TestWithBearer_MissingTokenReturnsConfigError(t *testing.T) {
	t.Parallel()
	env := mapEnv{}
	opt, err := withBearerFrom(env)
	if opt != nil {
		t.Errorf("expected nil Option; got %v", opt)
	}
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConfigError; got %T: %v", err, err)
	}
	if ce.Factory != "WithBearer" {
		t.Errorf("Factory = %q, want WithBearer", ce.Factory)
	}
	if len(ce.MissingEnv) != 1 || ce.MissingEnv[0] != EnvTokenVar {
		t.Errorf("MissingEnv = %v, want [%s]", ce.MissingEnv, EnvTokenVar)
	}
	if !strings.Contains(ce.Error(), EnvTokenVar) {
		t.Errorf("error message must name env var; got %q", ce.Error())
	}
}

func TestWithLicense_MissingEnvNamesBoth(t *testing.T) {
	t.Parallel()
	_, err := withLicenseFrom(mapEnv{})
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConfigError; got %T", err)
	}
	if ce.Factory != "WithLicense" {
		t.Errorf("Factory = %q", ce.Factory)
	}
	if len(ce.MissingEnv) != 2 {
		t.Errorf("MissingEnv = %v, want both keycode + email", ce.MissingEnv)
	}
}

func TestWithLicense_PresentValuesYieldOption_NewClientRejectsPending(t *testing.T) {
	t.Parallel()
	opt, err := withLicenseFrom(mapEnv{
		EnvLicenseKeycodeVar: "ABC-123-XYZ",
		EnvLicenseEmailVar:   "john.doe@acme-agency.com",
	})
	if err != nil {
		t.Fatalf("withLicenseFrom: %v", err)
	}
	if opt == nil {
		t.Fatalf("expected non-nil Option")
	}
	_, err = NewClient(opt)
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConfigError; got %T: %v", err, err)
	}
	if !strings.Contains(ce.Error(), "License auth mode") {
		t.Errorf("error must explain pending transport; got %q", ce.Error())
	}
}

func TestWithSession_MissingEnvNamesBoth(t *testing.T) {
	t.Parallel()
	_, err := withSessionFrom(mapEnv{})
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConfigError; got %T", err)
	}
	if len(ce.MissingEnv) != 2 {
		t.Errorf("MissingEnv = %v, want both id + secret", ce.MissingEnv)
	}
}

func TestWithSession_PresentValuesYieldOption_NewClientRejectsPending(t *testing.T) {
	t.Parallel()
	opt, err := withSessionFrom(mapEnv{
		EnvSessionIDVar:     "sess_abc",
		EnvSessionSecretVar: "secret_xyz",
	})
	if err != nil {
		t.Fatalf("withSessionFrom: %v", err)
	}
	_, err = NewClient(opt)
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ConfigError; got %T: %v", err, err)
	}
	if !strings.Contains(ce.Error(), "Session auth mode") {
		t.Errorf("error must explain pending transport; got %q", ce.Error())
	}
}

func TestConfigError_SatisfiesIsaError(t *testing.T) {
	t.Parallel()
	var ce IsaError = &ConfigError{Factory: "WithBearer", MissingEnv: []string{"X"}}
	if ce.IsaCode() != ErrorCodeUnspecified {
		t.Errorf("IsaCode() = %q, want empty", ce.IsaCode())
	}
}
