//go:build integration

package zyins

import (
	"context"
	"os"
	"testing"
	"time"
)

// integrationEnvTokenVar names the environment variable expected to
// hold a live `isa_test_*` bearer credential. The variable is set by
// CI; this file holds only the variable's NAME, never a literal value.
const integrationEnvTokenVar = "ZYINS_TEST_BEARER" //nolint:gosec // env var name only

// integrationEnvBaseURLVar optionally overrides the production base
// URL for staging integrations.
const integrationEnvBaseURLVar = "ZYINS_BASE_URL"

// integrationTimeout caps each integration test so a hung endpoint
// fails the suite rather than the CI runner.
const integrationTimeout = 30 * time.Second

func newIntegrationClient(t *testing.T) *Client {
	t.Helper()
	bearer := os.Getenv(integrationEnvTokenVar)
	if len(bearer) == 0 {
		t.Skipf("integration test skipped: %s not set", integrationEnvTokenVar)
	}
	opts := []Option{WithToken(bearer)}
	if base := os.Getenv(integrationEnvBaseURLVar); len(base) > 0 {
		opts = append(opts, WithBaseURL(base))
	}
	c, err := NewClient(opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestIntegration_UsageCurrent(t *testing.T) {
	c := newIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	snap, err := c.Usage.Current(ctx)
	if err != nil {
		t.Fatalf("Usage.Current: %v", err)
	}
	if len(snap.RequestID) == 0 {
		t.Errorf("expected RequestID from server, got empty")
	}
}

func TestIntegration_ReferenceStates(t *testing.T) {
	c := newIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	states, err := c.ReferenceData.States(ctx)
	if err != nil {
		t.Fatalf("ReferenceData.States: %v", err)
	}
	if len(states) == 0 {
		t.Errorf("expected at least one state in response")
	}
}
