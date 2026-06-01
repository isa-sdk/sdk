package zyins

import (
	"testing"
)

func TestCredentialState_MintsDeviceID(t *testing.T) {
	s, err := NewCredentialState(CredentialSnapshot{Keycode: "K", Email: "e@x"}, nil)
	if err != nil {
		t.Fatalf("NewCredentialState: %v", err)
	}
	if s.Snapshot().DeviceID == "" {
		t.Error("expected auto-minted DeviceID")
	}
}

func TestCredentialState_RefreshTriggersListener(t *testing.T) {
	s, err := NewCredentialState(CredentialSnapshot{Keycode: "K", Email: "e@x"}, nil)
	if err != nil {
		t.Fatalf("NewCredentialState: %v", err)
	}
	var got LicenseRefreshedEvent
	unsub := s.OnLicenseRefreshed(func(e LicenseRefreshedEvent) { got = e })
	defer unsub()
	if err := s.RefreshLicenseKey("lk-new"); err != nil {
		t.Fatalf("RefreshLicenseKey: %v", err)
	}
	if got.LicenseKey != "lk-new" {
		t.Errorf("listener.LicenseKey=%q", got.LicenseKey)
	}
	if s.Snapshot().LicenseKey != "lk-new" {
		t.Errorf("Snapshot.LicenseKey=%q", s.Snapshot().LicenseKey)
	}
}

func TestCredentialState_ClearRemovesKey(t *testing.T) {
	s, err := NewCredentialState(CredentialSnapshot{Keycode: "K", Email: "e@x", LicenseKey: "lk-old"}, nil)
	if err != nil {
		t.Fatalf("NewCredentialState: %v", err)
	}
	if err := s.ClearLicenseKey(); err != nil {
		t.Fatalf("ClearLicenseKey: %v", err)
	}
	if s.Snapshot().LicenseKey != "" {
		t.Error("expected cleared license key")
	}
}
