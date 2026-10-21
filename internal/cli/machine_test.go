package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jogvan-k/fit-agent/internal/config"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/syncorch"
)

func TestMachineErrorsNeverContainPrivateText(t *testing.T) {
	for _, tc := range []struct {
		err   error
		layer string
	}{
		{fmt.Errorf("private-canary: %w", icu.ErrUnauthorized), "Intervals authentication"},
		{fmt.Errorf("private-canary: %w", syncorch.ErrConflict), "sync conflict"},
		{fmt.Errorf("private-canary: %w", syncorch.ErrStalePreview), "sync conflict"},
		{&layerError{layer: "profile/config", err: errors.New("private-canary")}, "profile/config"},
		{errors.New("private-canary"), "validation"},
	} {
		cmd := NewRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		err := machineResult(cmd, nil, tc.err)
		if err == nil || strings.Contains(err.Error()+out.String(), "private-canary") {
			t.Fatal("private error leaked")
		}
		var envelope struct {
			SchemaVersion int  `json:"schemaVersion"`
			OK            bool `json:"ok"`
			Error         struct {
				Layer string `json:"layer"`
			} `json:"error"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.SchemaVersion != 1 || envelope.OK || envelope.Error.Layer != tc.layer {
			t.Fatalf("unexpected output: %s", out.String())
		}
	}
}

func TestFetchJSONAuthenticationFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("FIT_AGENT_PROFILE", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("private-canary"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ICU_BASE_URL", srv.URL)
	cfg := config.Config{Profiles: map[string]config.Profile{"plugin-test": {Workspace: root, IcuAthleteID: "0", IcuTimezone: "Europe/Copenhagen", IcuAPIKey: "dummy"}}}
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fetch", "--json", "--profile", "plugin-test", "--since", "1d"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("authentication failure reported success")
	}
	if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), "Intervals authentication") || strings.Contains(out.String(), "private-canary") {
		t.Fatalf("unexpected result: %s", out.String())
	}
}

func TestIntegrationInfoAndDoctorWorkspaceAssertion(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("FIT_AGENT_PROFILE", "")
	ws := filepath.Join(root, "workspace")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Profiles: map[string]config.Profile{"plugin-test": {Workspace: ws, IcuAthleteID: "0", IcuTimezone: "Europe/Copenhagen", IcuAPIKey: "private-canary"}}}
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args     []string
		fail     bool
		contains string
	}{
		{[]string{"integration-info", "--json"}, false, "guarded-sync-v1"},
		{[]string{"doctor", "--json", "--profile", "plugin-test", "--workspace", ws}, false, "credentialsReady"},
		{[]string{"doctor", "--json", "--profile", "plugin-test", "--workspace", root}, true, "workspace"},
		{[]string{"sync-workouts", "--json", "--dry-run"}, true, "validation"},
	} {
		cmd := NewRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(tc.args)
		err := cmd.Execute()
		if (err != nil) != tc.fail {
			t.Fatalf("args %v: %v", tc.args, err)
		}
		if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), tc.contains) || strings.Contains(out.String(), "private-canary") {
			t.Fatalf("bad machine output %s", out.String())
		}
	}
}
