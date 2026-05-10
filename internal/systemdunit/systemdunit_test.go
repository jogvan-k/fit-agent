package systemdunit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParamsValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		p       Params
		wantErr string
	}{
		{"ok", Params{Binary: "/usr/local/bin/fit-agent"}, ""},
		{"empty binary", Params{}, "binary path is required"},
		{"relative binary", Params{Binary: "fit-agent"}, "must be absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestRenderBasic(t *testing.T) {
	t.Parallel()
	got, err := RenderString(Params{Binary: "/usr/local/bin/fit-agent"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Description=fit-agent intervals.icu polling daemon",
		"ExecStart=/usr/local/bin/fit-agent serve",
		"Type=simple",
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered unit missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "WorkingDirectory=") {
		t.Errorf("expected no WorkingDirectory= when unset:\n%s", got)
	}
}

func TestRenderWithProfileAndArgs(t *testing.T) {
	t.Parallel()
	got, err := RenderString(Params{
		Binary:     "/opt/bin/fit-agent",
		Profile:    "home",
		ExtraArgs:  []string{"--quiet-hours", "23:00-06:00"},
		WorkingDir: "/home/me/coach",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "ExecStart=/opt/bin/fit-agent --profile home serve --quiet-hours 23:00-06:00"
	if !strings.Contains(got, want) {
		t.Errorf("missing ExecStart line %q\n---\n%s", want, got)
	}
	if !strings.Contains(got, "WorkingDirectory=/home/me/coach") {
		t.Errorf("missing WorkingDirectory line:\n%s", got)
	}
}

func TestRenderQuotesPathsWithSpaces(t *testing.T) {
	t.Parallel()
	got, err := RenderString(Params{Binary: "/Applications/My Apps/fit-agent"})
	if err != nil {
		t.Fatal(err)
	}
	want := `ExecStart="/Applications/My Apps/fit-agent" serve`
	if !strings.Contains(got, want) {
		t.Errorf("missing quoted ExecStart %q\n---\n%s", want, got)
	}
}

func TestInstallAndUninstallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := Install(Params{Binary: "/usr/local/bin/fit-agent"})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(dir, "systemd", "user", UnitName)
	if path != wantPath {
		t.Fatalf("install path = %q, want %q", path, wantPath)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ExecStart=/usr/local/bin/fit-agent serve") {
		t.Errorf("installed unit body unexpected:\n%s", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("perm = %v, want 0644", info.Mode().Perm())
	}

	// First uninstall removes; second is a no-op.
	removed, err := Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Errorf("first uninstall should report removed=true")
	}
	removed, err = Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Errorf("second uninstall should report removed=false")
	}
}

func TestUnitPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	got, err := UnitPath()
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/xdg-test/systemd/user/" + UnitName
	if got != want {
		t.Errorf("UnitPath() = %q, want %q", got, want)
	}
}

func TestQuoteHelper(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"plain":            "plain",
		"":                 `""`,
		"with space":       `"with space"`,
		`embedded "quote"`: `"embedded \"quote\""`,
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %q, want %q", in, got, want)
		}
	}
}
