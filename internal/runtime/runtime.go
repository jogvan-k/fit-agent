// Package runtime resolves the per-invocation context every fit-agent
// command needs: the active profile, the OS-keyring or fallback API
// key, an [icu.Client] configured for the athlete's timezone, and the
// [workspace.Layout] for the workspace pointed at by the profile.
//
// The resolver layers values in this order, highest priority first:
//
//  1. --profile flag on the cobra command
//  2. FIT_AGENT_PROFILE env var
//  3. <cwd>/.fit-agent.toml profile pointer (if cwd is inside a workspace)
//  4. config.DefaultProfile ("default")
//
// Resolution failures are returned as errors with actionable messages
// (e.g. "no profile configured; run `fit-agent init`").
package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/jogvan-k/fit-agent/internal/config"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// Resolved is the bundle every command receives after Resolve succeeds.
type Resolved struct {
	// ProfileName is the name resolved from the lookup chain.
	ProfileName string
	// Profile is the loaded profile entry.
	Profile config.Profile
	// Layout points at the workspace named by the profile.
	Layout workspace.Layout
	// Location is the athlete's IANA timezone, parsed; falls back to
	// time.UTC with a warning when the profile has none.
	Location *time.Location
	// Client is an authenticated intervals.icu client.
	Client *icu.Client
	// APIKey is exposed for commands that need to re-create clients
	// (e.g. test harnesses); production code should use Client.
	APIKey string
	// KeyringFallback is true when the API key came from the config
	// file rather than the OS keyring; CLIs print a warning.
	KeyringFallback bool
}

// Options govern how Resolve behaves.
type Options struct {
	// ProfileFlag is the value of --profile (empty if unset).
	ProfileFlag string
	// SecretStore lets tests inject an in-memory store.
	SecretStore config.SecretStore
	// HTTPClient overrides the default HTTP client; tests use httptest.
	HTTPClient *http.Client
	// BaseURL overrides the icu API root (env var ICU_BASE_URL also
	// honoured). Empty means production.
	BaseURL string
	// CWD is the working directory used to look for a workspace
	// pointer file. Defaults to os.Getwd().
	CWD string
}

// Resolve loads the active profile and constructs the icu client.
//
// A non-nil ctx is propagated only to future I/O; Resolve itself does
// not block on the network.
func Resolve(ctx context.Context, opts Options) (*Resolved, error) {
	_ = ctx

	store := opts.SecretStore
	if store == nil {
		store = config.KeyringStore{}
	}

	cwd := opts.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get cwd: %w", err)
		}
	}

	name := resolveProfileName(opts.ProfileFlag, cwd)

	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return nil, fmt.Errorf("no fit-agent config; run `fit-agent init`")
		}
		return nil, err
	}
	prof, err := cfg.Get(name)
	if err != nil {
		return nil, fmt.Errorf("%w (configured profiles: %v)", err, profileNames(cfg))
	}
	if prof.Workspace == "" {
		return nil, fmt.Errorf("profile %q has no workspace; run `fit-agent init`", name)
	}

	apiKey, fromFile, err := config.LoadAPIKey(store, prof, name)
	if err != nil {
		return nil, fmt.Errorf("load API key for profile %q: %w", name, err)
	}

	loc := time.UTC
	if prof.IcuTimezone != "" {
		l, err := time.LoadLocation(prof.IcuTimezone)
		if err != nil {
			return nil, fmt.Errorf("load timezone %q: %w", prof.IcuTimezone, err)
		}
		loc = l
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("ICU_BASE_URL")
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	client, err := icu.NewClient(apiKey, icu.Options{
		BaseURL:    baseURL,
		HTTPClient: httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("create icu client: %w", err)
	}

	return &Resolved{
		ProfileName:     name,
		Profile:         prof,
		Layout:          workspace.New(prof.Workspace),
		Location:        loc,
		Client:          client,
		APIKey:          apiKey,
		KeyringFallback: fromFile,
	}, nil
}

// resolveProfileName implements the priority chain described on the
// package doc. It returns the first non-empty value, defaulting to
// [config.DefaultProfile].
func resolveProfileName(flag, cwd string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv(config.EnvProfile); env != "" {
		return env
	}
	if name, ok := readPointer(cwd); ok {
		return name
	}
	return config.DefaultProfile
}

// readPointer walks up from cwd looking for a `.fit-agent.toml` file.
// Returns the value of its `profile = "..."` key when found.
func readPointer(cwd string) (string, bool) {
	dir := cwd
	for {
		p := filepath.Join(dir, ".fit-agent.toml")
		if data, err := os.ReadFile(p); err == nil {
			var pointer struct {
				Profile string `toml:"profile"`
			}
			if _, err := toml.Decode(string(data), &pointer); err == nil && pointer.Profile != "" {
				return pointer.Profile, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func profileNames(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Profiles))
	for k := range cfg.Profiles {
		out = append(out, k)
	}
	return out
}
