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
	"strconv"
	"strings"

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
	// IcuTimezone is the IANA timezone reported by intervals.icu for the
	// athlete (e.g. "Europe/Madrid"). Cached here so commands can render
	// local timestamps without re-fetching /athlete/0.
	IcuTimezone string `toml:"icu_timezone,omitempty"`
	// IcuAPIKey is only set when the OS keyring is unavailable.
	// It MUST NOT be set if a keyring entry exists for this profile.
	IcuAPIKey string `toml:"icu_api_key,omitempty"`
	// AutoSplitDistance controls implicit splitting of long unsegmented
	// active laps in the generated activity YAML. Accepts values like
	// "1km" (default), "500m", "2km", or "none"/"null"/"" to disable.
	// When unset the default of 1 km is applied.
	AutoSplitDistance string `toml:"auto_split_distance,omitempty"`
}

// AutoSplitDistanceM parses the profile's AutoSplitDistance string and
// returns the threshold in metres and whether the feature is enabled.
// An empty / unset value returns (1000, true) — the 1 km default.
func (p Profile) AutoSplitDistanceM() (metres int, enabled bool) {
	if strings.TrimSpace(strings.ToLower(p.AutoSplitDistance)) == "" {
		return 1000, true // default
	}
	m, ok, err := ParseAutoSplitDistance(p.AutoSplitDistance)
	if err != nil {
		return 1000, true
	}
	return m, ok
}

// ParseAutoSplitDistance parses a human-friendly distance string into
// metres. Accepted formats: "1km", "500m", "2.5km", "" / "none" /
// "null" (disabled). Returns (0, false, nil) when disabled.
func ParseAutoSplitDistance(s string) (metres int, enabled bool, err error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "", "none", "null":
		return 0, false, nil
	}
	if strings.HasSuffix(s, "km") {
		v, e := strconv.ParseFloat(strings.TrimSuffix(s, "km"), 64)
		if e != nil || v <= 0 {
			return 0, false, fmt.Errorf("invalid auto_split_distance %q", s)
		}
		return int(v*1000 + 0.5), true, nil
	}
	if strings.HasSuffix(s, "m") {
		v, e := strconv.ParseFloat(strings.TrimSuffix(s, "m"), 64)
		if e != nil || v <= 0 {
			return 0, false, fmt.Errorf("invalid auto_split_distance %q", s)
		}
		return int(v + 0.5), true, nil
	}
	return 0, false, fmt.Errorf("invalid auto_split_distance %q: use \"1km\", \"500m\", or \"none\"", s)
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
