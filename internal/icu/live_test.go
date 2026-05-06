//go:build live

// Live integration probe for the intervals.icu client. Excluded from
// normal builds and CI by the `live` build tag. Run manually with:
//
//	ICU_API_KEY=... go test -tags live ./internal/icu -run TestLiveProbe -v
//
// The output is only printed; nothing is asserted, so the test cannot
// fail. It exists to confirm the wire-shape of intervals.icu responses
// against the real account during development.
package icu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestLiveProbe(t *testing.T) {
	apiKey := os.Getenv("ICU_API_KEY")
	if apiKey == "" {
		t.Skip("ICU_API_KEY not set")
	}
	c, err := NewClient(apiKey, Options{
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
	t.Logf("athlete id=%s name=%s tz=%s ftp=%d lthr=%d", a.ID, a.Name, a.Timezone, a.FTP, a.LTHR)

	now := time.Now()
	from := now.AddDate(0, 0, -30).Format("2006-01-02")
	to := now.Format("2006-01-02")
	acts, err := c.ListActivities(ctx, a.ID, from, to)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	t.Logf("activities last 30d: %d", len(acts))
	for i, x := range acts {
		if i >= 3 {
			break
		}
		t.Logf("  - %s %s %s dist=%.0fm time=%ds hr=%d desc=%q",
			x.ID, x.StartDateLocal, x.Type, x.Distance, x.MovingTime, x.AverageHR, x.Description)
	}

	from7 := now.AddDate(0, 0, -7).Format("2006-01-02")
	w, err := c.ListWellnessRaw(ctx, a.ID, from7, to)
	if err != nil {
		t.Fatalf("ListWellnessRaw: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(w, &rows); err != nil {
		t.Fatalf("decode raw wellness: %v", err)
	}
	t.Logf("wellness raw rows: %d", len(rows))
	if len(rows) > 0 {
		// dump the most recent populated row's keys
		t.Log("sample wellness row keys:")
		keys := make([]string, 0, len(rows[len(rows)-1]))
		for k := range rows[len(rows)-1] {
			keys = append(keys, k)
		}
		t.Logf("  %v", keys)
		// and a non-empty value for each key
		for k, v := range rows[len(rows)-1] {
			t.Logf("    %s = %v", k, v)
		}
	}

	wd, err := c.ListWellness(ctx, a.ID, from7, to)
	if err != nil {
		t.Fatalf("ListWellness typed: %v", err)
	}
	t.Logf("typed wellness rows: %d", len(wd))
	for i, d := range wd {
		if i >= 3 {
			break
		}
		t.Logf("  %s rhr=%d hrv=%.1f sleepSecs=%.0f weight=%.2f stress=%.1f",
			d.ID, d.RestingHR, d.HRV, d.Sleep, d.Weight, d.Stress)
	}

	if len(acts) > 0 {
		id := acts[0].ID
		raw, err := c.GetActivityRaw(ctx, id)
		if err != nil {
			t.Fatalf("GetActivityRaw: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		t.Logf("activity %s has %d JSON keys", id, len(m))
		fmt.Printf("activity %s description=%q\n", id, m["description"])
		// download FIT to /tmp
		fp, err := os.CreateTemp("", "live-*.fit")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(fp.Name())
		if err := c.GetActivityFIT(ctx, id, fp); err != nil {
			fp.Close()
			t.Fatalf("GetActivityFIT: %v", err)
		}
		fp.Close()
		st, _ := os.Stat(fp.Name())
		t.Logf("downloaded fit %s (%d bytes)", fp.Name(), st.Size())
	}
}
