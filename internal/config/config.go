// Package config handles loading and saving the fit-agent configuration
// file and resolving the active profile.
//
// Configuration lives at ${XDG_CONFIG_HOME:-~/.config}/fit-agent/config.toml
// with mode 0600. Secrets (intervals.icu API keys) are stored in the OS
// keyring by default; see [Keyring] for the fallback path.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// DefaultProfile is the profile name used when nothing is specified.
const DefaultProfile = "default"

// EnvProfile is the environment variable that overrides the profile.
const EnvProfile = "FIT_AGENT_PROFILE"

// fileMode is the permission mode for the configuration file. It must
// not be widened: the file may contain an API key in the keyring-fallback
// path.
const fileMode os.FileMode = 0o600

// dirMode is the permission mode for created config directories.
const dirMode os.FileMode = 0o700

// Profile is a single named fit-agent configuration.
type Profile struct {
	// Workspace is the absolute path to the OpenClaw workspace.
	Workspace string `toml:"workspace"`
	// IcuAthleteID is the intervals.icu athlete identifier (e.g. "i12345").
	IcuAthleteID string `toml:"icu_athlete_id"`
	// IcuAPIKey is only set when the OS keyring is unavailable.
	// It MUST NOT be set if a keyring entry exists for this profile.
	IcuAPIKey string `toml:"icu_api_key,omitempty"`
}

// Config is the on-disk schema for the fit-agent configuration file.
type Config struct {
	Profiles map[string]Profile `toml:"profile"`
}

// ErrNotFound is returned when the configuration file does not exist.
var ErrNotFound = errors.New("config file not found")

// ErrUnknownProfile is returned when a requested profile is not present.
var ErrUnknownProfile = errors.New("unknown profile")

// Path returns the absolute path of the config file, creating no
// directories. It honors XDG_CONFIG_HOME and falls back to ~/.config.
func Path() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "fit-agent", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "fit-agent", "config.toml"), nil
}

// Load reads the configuration file. Returns [ErrNotFound] wrapped in a
// fs error if the file does not exist. Unknown TOML keys are ignored so
// older binaries can read newer configs.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(p)
}

// LoadFrom reads the configuration file at the given path.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return &cfg, nil
}

// Save writes the configuration to the default path with mode 0600,
// creating parent directories as needed. The write is atomic.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	return c.SaveTo(p)
}

// SaveTo writes the configuration to the given path with mode 0600,
// creating parent directories as needed. The write is atomic.
func (c *Config) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(c); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode config: %w", err)
	}
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp to config: %w", err)
	}
	return nil
}

// Get returns the named profile, or [ErrUnknownProfile].
func (c *Config) Get(name string) (Profile, error) {
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("%w: %q", ErrUnknownProfile, name)
	}
	return p, nil
}

// Set inserts or replaces a profile.
func (c *Config) Set(name string, p Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = p
}
