package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const sampleFIT = "../../testdata/fit/sample-intervals.fit"

func TestFitSummary(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fit", "summary", sampleFIT})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"sport:", "elapsed:", "laps:"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestFitSummaryJSON(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fit", "summary", "--json", sampleFIT})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var v summaryView
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if v.Sport == "" {
		t.Errorf("sport empty: %+v", v)
	}
	if v.Laps < 1 {
		t.Errorf("laps = %d", v.Laps)
	}
}

func TestFitLapsTable(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fit", "laps", sampleFIT})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header + at least one row, got %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "intensity") {
		t.Errorf("header missing column: %q", lines[0])
	}
}

func TestFitDumpJSON(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fit", "dump", sampleFIT})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := v["Laps"]; !ok {
		t.Errorf("dump missing Laps key, got keys: %v", keysOf(v))
	}
}

func TestFitMissingFile(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fit", "summary", "no-such.fit"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := formatPace(345); got != "5:45/km" {
		t.Errorf("formatPace(345) = %q", got)
	}
	if got := formatPace(0); got != "-" {
		t.Errorf("formatPace(0) = %q", got)
	}
	if got := blank(""); got != "-" {
		t.Errorf("blank(\"\") = %q", got)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
