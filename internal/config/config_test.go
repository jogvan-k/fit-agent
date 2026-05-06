package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPathRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/xdg/fit-agent/config.toml"
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/home/.config/fit-agent/config.toml"
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")

	in := &Config{Profiles: map[string]Profile{
		"default": {
			Workspace:    "/home/me/ws",
			IcuAthleteID: "i12345",
		},
		"alt": {
			Workspace:    "/home/me/other",
			IcuAthleteID: "i999",
			IcuAPIKey:    "fallback-key",
		},
	}}
	if err := in.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 0600", mode)
	}

	out, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out.Profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(out.Profiles))
	}
	if got := out.Profiles["default"]; got != in.Profiles["default"] {
		t.Errorf("default profile mismatch: got %+v, want %+v", got, in.Profiles["default"])
	}
	if got := out.Profiles["alt"]; got != in.Profiles["alt"] {
		t.Errorf("alt profile mismatch: got %+v, want %+v", got, in.Profiles["alt"])
	}
}

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadFrom(filepath.Join(dir, "missing.toml"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestLoadIgnoresUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[profile.default]
workspace = "/x"
icu_athlete_id = "i1"
future_field = "ignored"

[unrelated]
also = "ignored"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Profiles["default"].Workspace != "/x" {
		t.Errorf("workspace = %q, want /x", cfg.Profiles["default"].Workspace)
	}
}

func TestGetUnknownProfile(t *testing.T) {
	c := &Config{Profiles: map[string]Profile{"default": {}}}
	if _, err := c.Get("missing"); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("got %v, want ErrUnknownProfile", err)
	}
}

func TestResolveProfilePrecedence(t *testing.T) {
	t.Setenv(EnvProfile, "")
	dir := t.TempDir()
	if err := SaveWorkspacePointer(dir, WorkspacePointer{Profile: "from-ws"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		flag      string
		env       string
		workspace string
		want      string
	}{
		{"flag wins", "from-flag", "from-env", dir, "from-flag"},
		{"env beats workspace", "", "from-env", dir, "from-env"},
		{"workspace beats default", "", "", dir, "from-ws"},
		{"default fallback", "", "", "", DefaultProfile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvProfile, tc.env)
			got, err := ResolveProfile(tc.flag, tc.workspace)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("ResolveProfile(%q,%q) = %q, want %q", tc.flag, tc.workspace, got, tc.want)
			}
		})
	}
}

func TestWorkspacePointerMissing(t *testing.T) {
	dir := t.TempDir()
	p, err := LoadWorkspacePointer(dir)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}
	if p.Profile != "" {
		t.Errorf("expected empty profile, got %q", p.Profile)
	}
}
