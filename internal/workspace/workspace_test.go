package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLayoutPaths(t *testing.T) {
	l := New("/ws")
	cases := map[string]string{
		"FitAgentDir":        l.FitAgentDir(),
		"ActivitiesDir":      l.ActivitiesDir(),
		"WellnessDir":        l.WellnessDir(),
		"PlannedWorkoutsDir": l.PlannedWorkoutsDir(),
		"CacheDir":           l.CacheDir(),
		"CacheActivitiesDir": l.CacheActivitiesDir(),
		"CacheWellnessDir":   l.CacheWellnessDir(),
		"CacheEventsDir":     l.CacheEventsDir(),
		"CacheAthletePath":   l.CacheAthletePath(),
		"AthleteProfilePath": l.AthleteProfilePath(),
		"TrainingPlanPath":   l.TrainingPlanPath(),
		"ReadmePath":         l.ReadmePath(),
		"SkillsDir":          l.SkillsDir(),
		"PointerPath":        l.PointerPath(),
	}
	want := map[string]string{
		"FitAgentDir":        "/ws/fit-agent",
		"ActivitiesDir":      "/ws/fit-agent/activities",
		"WellnessDir":        "/ws/fit-agent/wellness",
		"PlannedWorkoutsDir": "/ws/fit-agent/planned-workouts",
		"CacheDir":           "/ws/fit-agent/.cache",
		"CacheActivitiesDir": "/ws/fit-agent/.cache/activities",
		"CacheWellnessDir":   "/ws/fit-agent/.cache/wellness",
		"CacheEventsDir":     "/ws/fit-agent/.cache/events",
		"CacheAthletePath":   "/ws/fit-agent/.cache/athlete.json",
		"AthleteProfilePath": "/ws/ATHLETE-PROFILE.md",
		"TrainingPlanPath":   "/ws/TRAINING-PLAN.md",
		"ReadmePath":         "/ws/README.md",
		"SkillsDir":          "/ws/skills",
		"PointerPath":        "/ws/.fit-agent.toml",
	}
	for k, got := range cases {
		if got != want[k] {
			t.Errorf("%s = %q, want %q", k, got, want[k])
		}
	}
}

func TestLayoutDatedPaths(t *testing.T) {
	l := New("/ws")
	d := time.Date(2026, 5, 3, 7, 30, 0, 0, time.UTC)
	if got, want := l.ActivityDayPath(d), "/ws/fit-agent/activities/2026-05-03.yaml"; got != want {
		t.Errorf("ActivityDayPath = %q, want %q", got, want)
	}
	if got, want := l.WellnessMonthPath(d), "/ws/fit-agent/wellness/2026-05.yaml"; got != want {
		t.Errorf("WellnessMonthPath = %q, want %q", got, want)
	}
	if got, want := l.PlannedWorkoutDayPath(d), "/ws/fit-agent/planned-workouts/2026-05-03.md"; got != want {
		t.Errorf("PlannedWorkoutDayPath = %q, want %q", got, want)
	}
	if got, want := l.CacheActivityJSONPath("i12345"), "/ws/fit-agent/.cache/activities/i12345.json"; got != want {
		t.Errorf("CacheActivityJSONPath = %q, want %q", got, want)
	}
	if got, want := l.CacheActivityFITPath("i12345"), "/ws/fit-agent/.cache/activities/i12345.fit"; got != want {
		t.Errorf("CacheActivityFITPath = %q, want %q", got, want)
	}
	if got, want := l.CacheWellnessMonthPath(d), "/ws/fit-agent/.cache/wellness/2026-05.json"; got != want {
		t.Errorf("CacheWellnessMonthPath = %q, want %q", got, want)
	}
	if got, want := l.CacheEventPath("i999"), "/ws/fit-agent/.cache/events/i999.json"; got != want {
		t.Errorf("CacheEventPath = %q, want %q", got, want)
	}
}

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	if err := l.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, d := range l.MachineDirs() {
		st, err := os.Stat(d)
		if err != nil {
			t.Errorf("missing %s: %v", d, err)
			continue
		}
		if !st.IsDir() {
			t.Errorf("%s is not a dir", d)
		}
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "file.txt")
	if err := AtomicWrite(target, []byte("hello"), 0); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != DefaultFileMode {
		t.Errorf("mode = %v, want %v", st.Mode().Perm(), DefaultFileMode)
	}
	// no leftover temp file
	entries, _ := os.ReadDir(filepath.Dir(target))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".fit-agent-") {
			t.Errorf("leftover temp: %s", e.Name())
		}
	}
}

func TestAtomicWriteOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := AtomicWrite(target, []byte("v1"), 0); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(target, []byte("v2-longer"), 0); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "v2-longer" {
		t.Errorf("content = %q, want v2-longer", got)
	}
}

func TestAtomicWriteFromStream(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "stream.bin")
	if err := AtomicWriteFrom(target, strings.NewReader("streamed"), 0); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "streamed" {
		t.Errorf("content = %q", got)
	}
}

func TestClassify(t *testing.T) {
	l := New("/ws")
	cases := []struct {
		path string
		want Owner
	}{
		{"/ws/fit-agent/activities/2026-05-03.yaml", OwnerMachine},
		{"/ws/fit-agent/wellness/2026-05.yaml", OwnerMachine},
		{"/ws/fit-agent/.cache/activities/i1.json", OwnerMachine},
		{"/ws/fit-agent/.cache/athlete.json", OwnerMachine},
		{"/ws/fit-agent/planned-workouts/2026-05-04.md", OwnerShared},
		{"/ws/ATHLETE-PROFILE.md", OwnerAgent},
		{"/ws/TRAINING-PLAN.md", OwnerAgent},
		{"/ws/README.md", OwnerAgent},
		{"/ws/skills/training-plan-coach/SKILL.md", OwnerAgent},
		{"/ws/.fit-agent.toml", OwnerAgent},
		{"/ws/.gitignore", OwnerAgent},
		{"/ws/random.txt", OwnerUnknown},
		{"/somewhere/else/file", OwnerUnknown},
	}
	for _, tc := range cases {
		if got := l.Classify(tc.path); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestGuardWrite(t *testing.T) {
	l := New("/ws")
	// machine writer can write a machine file
	if err := l.GuardWrite("/ws/fit-agent/activities/x.yaml", OwnerMachine); err != nil {
		t.Errorf("machine writing machine path: unexpected err %v", err)
	}
	// machine writer cannot write an agent file
	err := l.GuardWrite("/ws/ATHLETE-PROFILE.md", OwnerMachine)
	if err == nil {
		t.Fatalf("expected ownership error")
	}
	if !errors.Is(err, ErrOwnership) {
		t.Errorf("err is not ErrOwnership: %v", err)
	}
	var oe *OwnershipError
	if !errors.As(err, &oe) {
		t.Fatalf("err is not *OwnershipError: %v", err)
	}
	if oe.Got != OwnerAgent || oe.Want != OwnerMachine {
		t.Errorf("OwnershipError fields: %+v", oe)
	}
	// shared path accepts both
	if err := l.GuardWrite("/ws/fit-agent/planned-workouts/d.md", OwnerMachine); err != nil {
		t.Errorf("machine writing shared: %v", err)
	}
	if err := l.GuardWrite("/ws/fit-agent/planned-workouts/d.md", OwnerShared); err != nil {
		t.Errorf("shared writing shared: %v", err)
	}
	// unknown paths rejected
	if err := l.GuardWrite("/ws/random.txt", OwnerMachine); err == nil {
		t.Errorf("expected ownership error for unknown path")
	}
}
