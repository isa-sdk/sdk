package zyins

import (
	"fmt"
	"os"
	"sync"
)

// CredentialStore is the persistence facade used by CredentialState.
// Implementations MUST be safe for concurrent use. The default
// in-process implementation is MemoryCredentialStore; production
// callers swap in a backing AsyncStorage / file / keychain adapter.
type CredentialStore interface {
	Get(key string) (string, bool)
	Set(key, value string) error
	Remove(key string) error
}

// Credential keys persisted by CredentialState. Stable enums so
// integrators don't hand-roll the literal in five places.
const (
	CredentialKeyLicenseKey = "isa.licenseKey"
	CredentialKeyDeviceID   = "isa.deviceId"
	CredentialKeyKeycode    = "isa.keycode"
	CredentialKeyEmail      = "isa.email"
)

// LicenseRefreshedEvent is the payload fired when the SDK observes a
// fresh license key (typically a return value of License.Activate).
type LicenseRefreshedEvent struct {
	LicenseKey string
	DeviceID   string
	Email      string
	OrderID    string
}

// LicenseRefreshedListener subscribes to LicenseRefreshedEvent.
type LicenseRefreshedListener func(LicenseRefreshedEvent)

// CredentialSnapshot is the bootstrap input for NewCredentialState.
// Empty fields fall back to the store, then (for DeviceID) to an
// auto-minted value.
type CredentialSnapshot struct {
	Keycode    string
	Email      string
	DeviceID   string
	LicenseKey string
}

// CredentialState is the per-process credential snapshot shared between
// the License-HMAC sub-clients and the License ergonomic facade. The
// reference is stable across the SDK lifetime; the inner fields are
// mutated in place by Refresh* calls so every sub-client observes new
// credentials without re-bootstrap.
type CredentialState struct {
	mu         sync.RWMutex
	licenseKey string
	deviceID   string
	keycode    string
	email      string

	store     CredentialStore
	listeners []LicenseRefreshedListener
}

// NewCredentialState constructs a CredentialState from a snapshot. The
// store is consulted for persistent values (license key, device id)
// during construction; explicit fields in the snapshot win.
func NewCredentialState(snap CredentialSnapshot, store CredentialStore) (*CredentialState, error) {
	if store == nil {
		store = NewMemoryCredentialStore()
	}
	s := &CredentialState{
		licenseKey: snap.LicenseKey,
		deviceID:   snap.DeviceID,
		keycode:    snap.Keycode,
		email:      snap.Email,
		store:      store,
	}
	if s.licenseKey == "" {
		if v, ok := store.Get(CredentialKeyLicenseKey); ok {
			s.licenseKey = v
		}
	}
	if s.deviceID == "" {
		if v, ok := store.Get(CredentialKeyDeviceID); ok {
			s.deviceID = v
		}
	}
	if s.deviceID == "" {
		mintedID, err := mintDeviceID()
		if err != nil {
			return nil, fmt.Errorf("zyins: NewCredentialState minting deviceId: %w", err)
		}
		s.deviceID = mintedID
		if err := store.Set(CredentialKeyDeviceID, mintedID); err != nil {
			return nil, fmt.Errorf("zyins: NewCredentialState persisting deviceId: %w", err)
		}
	}
	if s.keycode == "" {
		if v, ok := store.Get(CredentialKeyKeycode); ok {
			s.keycode = v
		}
	}
	if s.email == "" {
		if v, ok := store.Get(CredentialKeyEmail); ok {
			s.email = v
		}
	}
	if snap.Keycode != "" {
		if err := store.Set(CredentialKeyKeycode, snap.Keycode); err != nil {
			return nil, fmt.Errorf("zyins: NewCredentialState persisting keycode: %w", err)
		}
	}
	if snap.Email != "" {
		if err := store.Set(CredentialKeyEmail, snap.Email); err != nil {
			return nil, fmt.Errorf("zyins: NewCredentialState persisting email: %w", err)
		}
	}
	return s, nil
}

// Snapshot returns the current credential values. The orderId field
// defaults to keycode when no explicit orderId was provided.
func (s *CredentialState) Snapshot() CredentialSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return CredentialSnapshot{
		Keycode:    s.keycode,
		Email:      s.email,
		DeviceID:   s.deviceID,
		LicenseKey: s.licenseKey,
	}
}

// OnLicenseRefreshed registers a listener and returns an unsubscribe
// function. Subscribers receive the fresh license key on every
// successful License.Activate.
func (s *CredentialState) OnLicenseRefreshed(listener LicenseRefreshedListener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
	idx := len(s.listeners) - 1
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if idx < len(s.listeners) {
			s.listeners[idx] = nil
		}
	}
}

// RefreshLicenseKey writes a fresh license key to the in-memory state,
// persists it through the credential store, and notifies subscribers.
// Listener failures are recovered so one observer does not break the
// activation flow.
func (s *CredentialState) RefreshLicenseKey(licenseKey string) error {
	s.mu.Lock()
	s.licenseKey = licenseKey
	deviceID := s.deviceID
	email := s.email
	keycode := s.keycode
	listeners := append([]LicenseRefreshedListener(nil), s.listeners...)
	s.mu.Unlock()
	if err := s.store.Set(CredentialKeyLicenseKey, licenseKey); err != nil {
		return fmt.Errorf("zyins: RefreshLicenseKey persisting license key: %w", err)
	}
	event := LicenseRefreshedEvent{
		LicenseKey: licenseKey,
		DeviceID:   deviceID,
		Email:      email,
		OrderID:    keycode,
	}
	for _, listener := range listeners {
		if listener == nil {
			continue
		}
		notifyListener(listener, event)
	}
	return nil
}

// ClearLicenseKey wipes the stashed license key. Called after a
// successful License.Deactivate.
func (s *CredentialState) ClearLicenseKey() error {
	s.mu.Lock()
	s.licenseKey = ""
	s.mu.Unlock()
	if err := s.store.Remove(CredentialKeyLicenseKey); err != nil {
		return fmt.Errorf("zyins: ClearLicenseKey removing persisted key: %w", err)
	}
	return nil
}

// notifyListener invokes one listener under a panic guard. Extracted so
// the iteration loop reads as a sequence of side effects rather than a
// nested anonymous function.
func notifyListener(listener LicenseRefreshedListener, event LicenseRefreshedEvent) {
	defer func() { _ = recover() }()
	listener(event)
}

// LicensesFromEnv reads ISA_LICENSE_KEYCODE and ISA_LICENSE_EMAIL from
// the environment and returns a partially-filled snapshot. Missing
// envs surface as a typed *ConfigError so callers see the gap at
// startup.
func LicensesFromEnv() (CredentialSnapshot, error) {
	keycode := os.Getenv(EnvLicenseKeycodeVar)
	email := os.Getenv(EnvLicenseEmailVar)
	if keycode == "" || email == "" {
		missing := []string{}
		if keycode == "" {
			missing = append(missing, EnvLicenseKeycodeVar)
		}
		if email == "" {
			missing = append(missing, EnvLicenseEmailVar)
		}
		return CredentialSnapshot{}, &ConfigError{Factory: "LicensesFromEnv", MissingEnv: missing}
	}
	return CredentialSnapshot{Keycode: keycode, Email: email}, nil
}

