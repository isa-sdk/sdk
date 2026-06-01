package zyins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth_GetReadiness_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != readinessPath {
			t.Errorf("path = %s, want %s", r.URL.Path, readinessPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ready": true,
			"status": "serving",
			"db": {"status":"serving","latency_ms":"3","checked_at":"2026-05-14T14:32:01Z"},
			"cache": {"status":"serving","latency_ms":1,"checked_at":"2026-05-14T14:32:01Z"},
			"checked_at": "2026-05-14T14:32:01Z"
		}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Health.GetReadiness(context.Background())
	if err != nil {
		t.Fatalf("GetReadiness: %v", err)
	}
	if !result.Ready {
		t.Errorf("ready = false")
	}
	if result.Status != ServingStatusServing {
		t.Errorf("status = %q, want serving", result.Status)
	}
	if result.DB.Status != ServingStatusServing || result.DB.LatencyMs != 3 {
		t.Errorf("db probe not parsed: %+v", result.DB)
	}
}

func TestHealth_GetReadiness_NotServing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"not ready","status":503,"code":"service_unavailable"}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Health.GetReadiness(context.Background())
	if err == nil {
		t.Fatalf("expected error from 503 response")
	}
}

func TestHealth_GetReadiness_DownstreamMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{
			"ready": false,
			"status": "not_serving",
			"db": {"status":"serving","latency_ms":3,"checked_at":"2026-05-14T14:32:01Z"},
			"cache": {"status":"not_serving","latency_ms":0,"message":"connection refused","checked_at":"2026-05-14T14:32:01Z"},
			"downstream_services": {
				"accounts": {"status":"serving","latency_ms":5,"checked_at":"2026-05-14T14:32:01Z"}
			},
			"checked_at": "2026-05-14T14:32:01Z"
		}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Health.GetReadiness(context.Background())
	if err != nil {
		t.Fatalf("GetReadiness: %v", err)
	}
	if result.Ready {
		t.Errorf("ready = true, want false")
	}
	if result.Cache.Message != "connection refused" {
		t.Errorf("cache.message = %q", result.Cache.Message)
	}
	if got := result.DownstreamServices["accounts"].LatencyMs; got != 5 {
		t.Errorf("downstream accounts latency = %d, want 5", got)
	}
}
