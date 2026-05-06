package config

import (
	"errors"
	"testing"
)

func TestMemoryStoreRoundTrip(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.Get("default"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("Get on empty: got %v, want ErrSecretNotFound", err)
	}
	if err := s.Set("default", "key123"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("default")
	if err != nil {
		t.Fatal(err)
	}
	if got != "key123" {
		t.Errorf("Get = %q, want key123", got)
	}
	if err := s.Delete("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("default"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("Get after Delete: got %v, want ErrSecretNotFound", err)
	}
	if err := s.Delete("default"); err != nil {
		t.Errorf("Delete on missing: %v", err)
	}
}

func TestLoadAPIKeyPrefersStore(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("default", "from-store")
	got, fallback, err := LoadAPIKey(s, Profile{IcuAPIKey: "from-file"}, "default")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-store" {
		t.Errorf("got %q, want from-store", got)
	}
	if fallback {
		t.Error("fallback = true, want false")
	}
}

func TestLoadAPIKeyFallsBackToFile(t *testing.T) {
	s := NewMemoryStore()
	got, fallback, err := LoadAPIKey(s, Profile{IcuAPIKey: "from-file"}, "default")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-file" {
		t.Errorf("got %q, want from-file", got)
	}
	if !fallback {
		t.Error("fallback = false, want true")
	}
}

func TestLoadAPIKeyMissingEverywhere(t *testing.T) {
	s := NewMemoryStore()
	if _, _, err := LoadAPIKey(s, Profile{}, "default"); err == nil {
		t.Error("expected error when no key anywhere")
	}
}
