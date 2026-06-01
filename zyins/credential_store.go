package zyins

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// MemoryCredentialStore is the default CredentialStore for
// short-running processes (tests, CLI tools). Production callers swap
// in a persistent backing store (file, keychain, AsyncStorage).
type MemoryCredentialStore struct {
	mu sync.RWMutex
	m  map[string]string
}

// NewMemoryCredentialStore constructs an empty in-process store.
func NewMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{m: map[string]string{}}
}

// Get returns the stored value for key plus a `found` flag.
func (s *MemoryCredentialStore) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	return v, ok
}

// Set stores value under key. Never returns an error in this impl.
func (s *MemoryCredentialStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

// Remove deletes key from the store. Idempotent.
func (s *MemoryCredentialStore) Remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// mintDeviceID generates a 32-character hex device id, matching the
// length and shape produced by the TS SDK's deviceId helper.
func mintDeviceID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("zyins: mintDeviceID rand.Read: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
