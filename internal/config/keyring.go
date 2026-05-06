package config

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// KeyringService is the service name used in the OS keyring.
const KeyringService = "fit-agent"

// ErrKeyringUnavailable is returned when no usable OS keyring backend is
// present on this system. Callers may fall back to storing the secret in
// the configuration file (with a visible warning to the user).
var ErrKeyringUnavailable = errors.New("OS keyring unavailable")

// ErrSecretNotFound is returned when no secret exists for the profile.
var ErrSecretNotFound = errors.New("secret not found")

// SecretStore stores and retrieves the intervals.icu API key per profile.
//
// The default implementation backs onto the OS keyring. Tests inject a
// memory-backed implementation.
type SecretStore interface {
	// Get returns the stored secret for the profile, or
	// [ErrSecretNotFound] if none exists.
	Get(profile string) (string, error)
	// Set stores the secret for the profile.
	Set(profile, secret string) error
	// Delete removes the secret for the profile. It is not an error
	// to delete a missing secret.
	Delete(profile string) error
}

// KeyringStore is the OS-keyring-backed [SecretStore].
type KeyringStore struct{}

// Get implements [SecretStore].
func (KeyringStore) Get(profile string) (string, error) {
	s, err := keyring.Get(KeyringService, profile)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("%w: profile %q", ErrSecretNotFound, profile)
		}
		return "", fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
	}
	return s, nil
}

// Set implements [SecretStore].
func (KeyringStore) Set(profile, secret string) error {
	if err := keyring.Set(KeyringService, profile, secret); err != nil {
		return fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
	}
	return nil
}

// Delete implements [SecretStore].
func (KeyringStore) Delete(profile string) error {
	err := keyring.Delete(KeyringService, profile)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
}

// MemoryStore is an in-memory [SecretStore] used by tests.
type MemoryStore struct {
	m map[string]string
}

// NewMemoryStore returns an empty memory-backed secret store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]string{}} }

// Get implements [SecretStore].
func (s *MemoryStore) Get(profile string) (string, error) {
	v, ok := s.m[profile]
	if !ok {
		return "", fmt.Errorf("%w: profile %q", ErrSecretNotFound, profile)
	}
	return v, nil
}

// Set implements [SecretStore].
func (s *MemoryStore) Set(profile, secret string) error {
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[profile] = secret
	return nil
}

// Delete implements [SecretStore].
func (s *MemoryStore) Delete(profile string) error {
	delete(s.m, profile)
	return nil
}

// LoadAPIKey returns the API key for the profile, preferring the keyring
// and falling back to the [Profile.IcuAPIKey] field. The boolean return
// indicates whether the key came from the fallback (file) path; callers
// typically warn the user when true.
func LoadAPIKey(store SecretStore, profile Profile, name string) (string, bool, error) {
	secret, err := store.Get(name)
	if err == nil {
		return secret, false, nil
	}
	if errors.Is(err, ErrSecretNotFound) || errors.Is(err, ErrKeyringUnavailable) {
		if profile.IcuAPIKey != "" {
			return profile.IcuAPIKey, true, nil
		}
		return "", false, fmt.Errorf("no API key for profile %q: %w", name, err)
	}
	return "", false, err
}
