//go:build live

// Live integration test that downloads a recent activity and renders
// it through internal/render. Output is logged for manual inspection.
//
//	ICU_API_KEY=... go test -tags live ./internal/render -run TestLiveRender -v
package render

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/fitparse"
	"github.com/jogvan-k/fit-agent/internal/icu"
)

func TestLiveRender(t *testing.T) {
	apiKey := os.Getenv("ICU_API_KEY")
	if apiKey == "" {
		t.Skip("ICU_API_KEY not set")
	}
	c, err := icu.NewClient(apiKey, icu.Options{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	a, err := c.GetAthlete(ctx, "0")
	if err != nil {
		t.Fatalf("GetAthlete: %v", err)
	}
	loc, err := time.LoadLocation(a.Timezone)
	if err != nil {
		loc = time.UTC
	}
	t.Logf("athlete %s tz=%s", a.ID, a.Timezone)

	now := time.Now()
	from := now.AddDate(0, 0, -7).Format("2006-01-02")
	to := now.Format("2006-01-02")
	acts, err := c.ListActivities(ctx, a.ID, from, to)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	if len(acts) == 0 {
		t.Skip("no recent activities")
	}
	// pick the most recent activity that has a FIT file
	var pick *icu.ActivitySummary
	for i := range acts {
		if acts[i].FileType == "fit" {
			pick = &acts[i]
			break
		}
	}
	if pick == nil {
		t.Skip("no recent activity with fit")
	}
	t.Logf("rendering %s %s %s", pick.ID, pick.StartDateLocal, pick.Type)

	tmp, err := os.CreateTemp("", "live-*.fit")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	if err := c.GetActivityFIT(ctx, pick.ID, tmp); err != nil {
		tmp.Close()
		t.Fatalf("GetActivityFIT: %v", err)
	}
	tmp.Close()

	parsed, err := fitparse.Decode(tmp.Name())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	t.Logf("parsed: sport=%s laps=%d intervals=%d duration=%s distance=%.0fm",
		parsed.Sport, len(parsed.Laps), len(parsed.Intervals), parsed.TotalTime, parsed.Distance)

	// parse the date from start_local
	date, err := time.ParseInLocation("2006-01-02T15:04:05", pick.StartDateLocal, loc)
	if err != nil {
		date = now
	}
	day := ActivityDay{
		Date:        time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc),
		GeneratedAt: now,
		Location:    loc,
		Activities:  []ActivityInput{{Summary: *pick, FIT: parsed}},
	}
	yaml, err := ActivityDayYAML(day)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "rendered.yaml")
	_ = os.WriteFile(out, yaml, 0o644)
	t.Logf("wrote %s (%d bytes)", out, len(yaml))
	t.Logf("--- yaml ---\n%s", string(yaml))
}
