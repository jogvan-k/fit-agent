package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runCmd(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestWorkoutParseStdin(t *testing.T) {
	src := "- 10m Z2\n- 5x (4m Z5 / 3m Z2)\n"
	out, _, err := runCmd(t, src, "workout", "parse", "-")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != src {
		t.Errorf("parse output mismatch:\n got: %q\nwant: %q", out, src)
	}
}

func TestWorkoutRenderStdin(t *testing.T) {
	src := "- 15m 55% -- Warmup\n- 3x (1m 150% / 1m 50%)\n- 5m 50%\n"
	out, _, err := runCmd(t, src, "workout", "render", "-")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "3x\n- 1m 150%\n- 1m 50%") {
		t.Errorf("render output missing repeat block:\n%s", out)
	}
	if !strings.Contains(out, "- 15m 55% Warmup") {
		t.Errorf("render output missing inline note:\n%s", out)
	}
}

func TestWorkoutLintError(t *testing.T) {
	_, _, err := runCmd(t, "- 5m foo\n", "workout", "lint", "-")
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if !strings.Contains(err.Error(), "unknown intensity") {
		t.Errorf("err=%q", err.Error())
	}
}

func TestWorkoutLintSummary(t *testing.T) {
	out, _, err := runCmd(t, "- 10m Z1\n- 5x (4m Z5 / 3m Z2)\n", "workout", "lint", "-")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "2 steps") || !strings.Contains(out, "zones=") {
		t.Errorf("unexpected summary: %q", out)
	}
}
