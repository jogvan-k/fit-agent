package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// stubAthleteServer returns an httptest server that responds to
// /api/v1/athlete/0 with a minimal valid Athlete JSON. Tests inject
// its URL via the ICU_BASE_URL env var.
func stubAthleteServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/athlete/0") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "i999",
			"name":     "Test Athlete",
			"timezone": "Europe/Madrid",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newScaffoldOpts(t *testing.T, srvURL, wsDir string, force bool) *initOptions {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ICU_BASE_URL", srvURL)
	opts := &initOptions{
		WorkspaceDir:   wsDir,
		APIKey:         "test-key",
		ProfileName:    "default",
		NonInteractive: true,
		Force:          force,
	}
	if err := opts.resolveDefaults(); err != nil {
		t.Fatalf("resolveDefaults: %v", err)
	}
	if err := opts.validateAPIKey(context.Background()); err != nil {
		t.Fatalf("validateAPIKey: %v", err)
	}
	return opts
}

func TestInitNonInteractiveScaffolds(t *testing.T) {
	srv := stubAthleteServer(t)
	wsDir := t.TempDir()

	opts := newScaffoldOpts(t, srv.URL, wsDir, false)
	layout := workspace.New(wsDir)
	plan, err := buildScaffoldPlan(layout, opts)
	if err != nil {
		t.Fatalf("buildScaffoldPlan: %v", err)
	}
	if err := executeScaffold(io.Discard, layout, plan, false, false); err != nil {
		t.Fatalf("executeScaffold: %v", err)
	}

	for _, p := range []string{
		layout.AthleteProfilePath(),
		layout.ReadmePath(),
		layout.PointerPath(),
		filepath.Join(wsDir, ".gitignore"),
		filepath.Join(layout.SkillsDir(), "training-plan-coach", "SKILL.md"),
		filepath.Join(layout.SkillsDir(), "training-session-coach", "SKILL.md"),
		filepath.Join(layout.SkillsDir(), "workout-builder", "SKILL.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing scaffold file %s: %v", p, err)
		}
	}
	for _, d := range layout.MachineDirs() {
		st, err := os.Stat(d)
		if err != nil {
			t.Errorf("missing machine dir %s: %v", d, err)
			continue
		}
		if !st.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}
	body, err := os.ReadFile(layout.PointerPath())
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if !strings.Contains(string(body), `profile = "default"`) {
		t.Errorf("pointer missing profile entry; got:\n%s", body)
	}
	if opts.AthleteID != "i999" || opts.Timezone != "Europe/Madrid" {
		t.Errorf("athlete metadata not captured: id=%q tz=%q", opts.AthleteID, opts.Timezone)
	}
}

func TestInitIdempotentKeepsExistingAgentFiles(t *testing.T) {
	srv := stubAthleteServer(t)
	wsDir := t.TempDir()
	layout := workspace.New(wsDir)

	want := []byte("# my hand-edited profile\n")
	if err := os.WriteFile(layout.AthleteProfilePath(), want, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := newScaffoldOpts(t, srv.URL, wsDir, false)
	plan, err := buildScaffoldPlan(layout, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeScaffold(io.Discard, layout, plan, false, false); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(layout.AthleteProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("ATHLETE-PROFILE.md was overwritten without --force\nwant: %q\ngot:  %q", want, got)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	srv := stubAthleteServer(t)
	wsDir := t.TempDir()
	layout := workspace.New(wsDir)

	if err := os.WriteFile(layout.AthleteProfilePath(), []byte("STALE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := newScaffoldOpts(t, srv.URL, wsDir, true)
	plan, err := buildScaffoldPlan(layout, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeScaffold(io.Discard, layout, plan, true, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(layout.AthleteProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "STALE\n" {
		t.Errorf("--force should overwrite, but stale content survived")
	}
}

func TestInitDryRunWritesNothing(t *testing.T) {
	srv := stubAthleteServer(t)
	wsDir := t.TempDir()
	layout := workspace.New(wsDir)
	opts := newScaffoldOpts(t, srv.URL, wsDir, false)
	plan, err := buildScaffoldPlan(layout, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeScaffold(io.Discard, layout, plan, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(layout.AthleteProfilePath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry-run created a file: %v", err)
	}
	for _, d := range layout.MachineDirs() {
		if _, err := os.Stat(d); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("dry-run created machine dir %s: %v", d, err)
		}
	}
}

func TestInitNonInteractiveRequiresAPIKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := NewRootCmd()
	root.SetArgs([]string{"init", "--non-interactive", "--workspace", t.TempDir(), "--skip-validation"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "api-key") {
		t.Errorf("want missing api-key error, got %v", err)
	}
}

func TestApplyEnvHonorsAPIKeyEnv(t *testing.T) {
	t.Setenv(EnvAPIKey, "from-env")
	opts := &initOptions{}
	if err := opts.applyEnv(); err != nil {
		t.Fatal(err)
	}
	if opts.APIKey != "from-env" {
		t.Errorf("want APIKey=from-env, got %q", opts.APIKey)
	}
}

func TestFirstWord(t *testing.T) {
	cases := map[string]string{
		"":              "Athlete",
		"  ":            "Athlete",
		"Jogvan":        "Jogvan",
		"Jogvan Knudal": "Jogvan",
		"  Jen Doe":     "Jen",
	}
	for in, want := range cases {
		if got := firstWord(in); got != want {
			t.Errorf("firstWord(%q) = %q, want %q", in, got, want)
		}
	}
}
