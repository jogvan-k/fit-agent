package serveorch

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	tcs := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"defaults", Config{}, false},
		{"below floor", Config{Interval: 1 * time.Minute}, true},
		{"quiet without window", Config{Interval: 15 * time.Minute, QuietInterval: 30 * time.Minute}, false},
		{"window without quiet interval", Config{Interval: 15 * time.Minute, QuietStart: "23:00", QuietEnd: "06:00"}, false},
		{"window only start", Config{Interval: 15 * time.Minute, QuietStart: "23:00"}, true},
		{"bad time", Config{Interval: 15 * time.Minute, QuietStart: "25:00", QuietEnd: "06:00"}, true},
		{"negative jitter", Config{Interval: 15 * time.Minute, Jitter: -1}, true},
		{"quiet below floor", Config{Interval: 15 * time.Minute, QuietInterval: 1 * time.Minute, QuietStart: "23:00", QuietEnd: "06:00"}, true},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.cfg
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestInQuietHours(t *testing.T) {
	cfg := Config{
		Interval:      15 * time.Minute,
		QuietInterval: 2 * time.Hour,
		QuietStart:    "23:00",
		QuietEnd:      "06:00",
		Location:      time.UTC,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	at := func(h, m int) time.Time {
		return time.Date(2026, 5, 10, h, m, 0, 0, time.UTC)
	}
	tcs := []struct {
		t    time.Time
		want bool
	}{
		{at(0, 0), true},   // just past midnight
		{at(5, 59), true},  // last quiet minute
		{at(6, 0), false},  // window is half-open
		{at(12, 0), false}, // midday
		{at(22, 59), false},
		{at(23, 0), true},
		{at(23, 30), true},
	}
	for _, tc := range tcs {
		if got := cfg.inQuietHours(tc.t); got != tc.want {
			t.Errorf("inQuietHours(%s) = %v, want %v", tc.t.Format("15:04"), got, tc.want)
		}
	}
}

func TestInQuietHoursDisabled(t *testing.T) {
	cfg := Config{Interval: 15 * time.Minute, Location: time.UTC}
	_ = cfg.Validate()
	if cfg.inQuietHours(time.Date(2026, 5, 10, 3, 0, 0, 0, time.UTC)) {
		t.Error("expected no quiet hours when QuietStart/End empty")
	}
}

func TestNextDelay(t *testing.T) {
	cfg := Config{
		Interval:      15 * time.Minute,
		QuietInterval: 2 * time.Hour,
		QuietStart:    "23:00",
		QuietEnd:      "06:00",
		Jitter:        0,
		Location:      time.UTC,
	}
	_ = cfg.Validate()
	r := rand.New(rand.NewSource(1))
	if got := cfg.nextDelay(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), r); got != 15*time.Minute {
		t.Errorf("daytime nextDelay = %s, want 15m", got)
	}
	if got := cfg.nextDelay(time.Date(2026, 5, 10, 3, 0, 0, 0, time.UTC), r); got != 2*time.Hour {
		t.Errorf("night nextDelay = %s, want 2h", got)
	}
}

func TestLoopRunsRequestedIterations(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	tick := func(ctx context.Context) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}
	// use a fake sleep so the test is instant
	sleep := func(ctx context.Context, d time.Duration) error { return nil }

	loop := &Loop{
		Cfg:   Config{Interval: 15 * time.Minute},
		Tick:  tick,
		Sleep: sleep,
		Now:   func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	}
	ticks, err := loop.Run(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if ticks != 3 || calls != 3 {
		t.Errorf("ticks=%d calls=%d, want 3/3", ticks, calls)
	}
}

func TestLoopSwallowsTickErrors(t *testing.T) {
	tick := func(ctx context.Context) error { return errors.New("boom") }
	var logged []string
	loop := &Loop{
		Cfg:   Config{Interval: 15 * time.Minute},
		Tick:  tick,
		Sleep: func(ctx context.Context, d time.Duration) error { return nil },
		Now:   func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
		Logger: func(format string, args ...any) {
			logged = append(logged, format)
		},
	}
	ticks, err := loop.Run(context.Background(), 2)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if ticks != 2 {
		t.Errorf("ticks=%d, want 2", ticks)
	}
	// Ensure at least one error was logged.
	var sawErr bool
	for _, f := range logged {
		if f == "tick error: %v" {
			sawErr = true
		}
	}
	if !sawErr {
		t.Errorf("expected tick errors to be logged; got %v", logged)
	}
}

func TestLoopExitsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tick := func(ctx context.Context) error {
		cancel() // cancel mid-tick
		return nil
	}
	loop := &Loop{
		Cfg:   Config{Interval: 15 * time.Minute},
		Tick:  tick,
		Sleep: func(ctx context.Context, d time.Duration) error { return ctx.Err() },
		Now:   func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	}
	ticks, err := loop.Run(ctx, 0)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if ticks != 1 {
		t.Errorf("expected exactly 1 tick before cancel, got %d", ticks)
	}
}

func TestLoopRequiresTick(t *testing.T) {
	loop := &Loop{Cfg: Config{Interval: 15 * time.Minute}}
	if _, err := loop.Run(context.Background(), 1); err == nil {
		t.Error("expected error when Tick is nil")
	}
}

func TestParseHM(t *testing.T) {
	cases := map[string]int{
		"00:00": 0,
		"06:30": 6*60 + 30,
		"23:59": 23*60 + 59,
	}
	for in, want := range cases {
		got, err := parseHM(in)
		if err != nil || got != want {
			t.Errorf("parseHM(%q)=%d,%v want %d,nil", in, got, err, want)
		}
	}
	bad := []string{"", "25:00", "12:60", "ab:cd", "12"}
	for _, in := range bad {
		if _, err := parseHM(in); err == nil {
			t.Errorf("parseHM(%q) expected error", in)
		}
	}
}
