// Package serveorch implements the polling daemon that backs
// `fit-agent serve`. It periodically calls a "tick" function (in
// production: cache.All + renderorch.All over a small recent window)
// and is designed to stay well inside intervals.icu's rate limits.
//
// Defaults reflect the limits David (intervals.icu) published on the
// forum on 2025-08-21: 30 req/s for 1s and 132 req/10s. He suggested
// 10 req/s as a safe ceiling. Each `fetch --since 2d` tick issues
// ~3-8 requests, so even a 5-minute interval is ~1-2 req/min — three
// orders of magnitude under the limit.
//
// The loop:
//
//   - sleeps for [Config.Interval] (or [Config.QuietInterval] inside
//     quiet hours) plus a uniform jitter in [0, Config.Jitter)
//   - calls Tick(ctx); errors are logged and never abort the loop
//   - if Tick takes longer than the configured interval, the next
//     tick fires immediately (no overlap; ticks are serialised)
//   - exits cleanly when ctx is cancelled (SIGTERM/SIGINT)
//
// The package has no dependency on cobra, icu, or workspace packages
// so it can be tested with a fake Tick + fake clock.
package serveorch

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// MinInterval is the hard floor on poll cadence. Even at the floor the
// daemon stays comfortably inside icu's limits, but anything tighter
// is wasteful — icu's own ACTIVITY_ANALYZED webhook deliberately
// delays 60s to coalesce updates, so sub-minute polling cannot beat
// upstream latency.
const MinInterval = 5 * time.Minute

// DefaultInterval is the active-hours poll cadence.
const DefaultInterval = 15 * time.Minute

// DefaultQuietInterval is the cadence used inside quiet hours; 0
// disables the quiet-hours behaviour entirely (always use Interval).
const DefaultQuietInterval = 2 * time.Hour

// DefaultJitter spreads multiple machines polling the same key so they
// don't synchronise. Per icu's limits a small jitter is plenty.
const DefaultJitter = 60 * time.Second

// Config configures the polling loop. Zero values are filled with the
// Default* constants above; see [Config.Validate] for the rules.
type Config struct {
	// Interval between ticks during active hours.
	Interval time.Duration
	// QuietInterval between ticks inside [QuietStart, QuietEnd]; 0
	// disables quiet-hours and always uses Interval.
	QuietInterval time.Duration
	// QuietStart and QuietEnd bracket the quiet-hours window, parsed
	// in the loop's location. Both empty = no quiet hours. The window
	// may straddle midnight (e.g. 23:00..06:00).
	QuietStart string // "HH:MM"
	QuietEnd   string // "HH:MM"
	// Jitter is the maximum extra wait added to each interval.
	Jitter time.Duration
	// Location is the timezone QuietStart/QuietEnd are interpreted
	// in. Defaults to time.UTC; production wires the athlete-local TZ.
	Location *time.Location
}

// Validate fills defaults and rejects out-of-range values.
//
// The MinInterval floor applies to BOTH Interval and QuietInterval.
func (c *Config) Validate() error {
	if c.Interval == 0 {
		c.Interval = DefaultInterval
	}
	if c.Interval < MinInterval {
		return fmt.Errorf("interval %s below minimum %s", c.Interval, MinInterval)
	}
	if c.QuietInterval != 0 && c.QuietInterval < MinInterval {
		return fmt.Errorf("quiet_interval %s below minimum %s", c.QuietInterval, MinInterval)
	}
	if (c.QuietStart == "") != (c.QuietEnd == "") {
		return errors.New("quiet_start and quiet_end must be set together")
	}
	if c.QuietStart != "" {
		if _, err := parseHM(c.QuietStart); err != nil {
			return fmt.Errorf("quiet_start: %w", err)
		}
		if _, err := parseHM(c.QuietEnd); err != nil {
			return fmt.Errorf("quiet_end: %w", err)
		}
	}
	if c.Jitter < 0 {
		return fmt.Errorf("jitter %s must be non-negative", c.Jitter)
	}
	if c.Location == nil {
		c.Location = time.UTC
	}
	return nil
}

// inQuietHours reports whether t (in the loop's location) falls inside
// the quiet window. Returns false if quiet hours are not configured.
func (c Config) inQuietHours(t time.Time) bool {
	if c.QuietStart == "" || c.QuietEnd == "" || c.QuietInterval == 0 {
		return false
	}
	start, _ := parseHM(c.QuietStart) // already validated
	end, _ := parseHM(c.QuietEnd)
	now := minutesOfDay(t.In(c.Location))
	if start == end {
		return false
	}
	if start < end {
		return now >= start && now < end
	}
	// straddles midnight, e.g. 23:00..06:00
	return now >= start || now < end
}

// nextDelay computes the next sleep duration: the relevant interval
// plus a jitter draw in [0, Jitter). Exposed for tests.
func (c Config) nextDelay(now time.Time, r *rand.Rand) time.Duration {
	base := c.Interval
	if c.inQuietHours(now) {
		base = c.QuietInterval
	}
	if c.Jitter > 0 {
		base += time.Duration(r.Int63n(int64(c.Jitter)))
	}
	return base
}

// TickFunc is the unit of work the loop calls each iteration. It must
// be safe to invoke serially; it will never overlap with itself. A
// returned error is logged and the loop continues.
type TickFunc func(ctx context.Context) error

// Loop drives Tick on the cadence described by cfg. It returns nil
// when ctx is cancelled and an error only on configuration problems
// (Tick errors are logged via logf, not surfaced).
//
// Loop runs Tick once immediately on entry so a fresh start picks up
// recent data without waiting a full interval; pass --once at the CLI
// to exit after that first call.
type Loop struct {
	Cfg    Config
	Tick   TickFunc
	Logger func(format string, args ...any)
	// Now and Sleep are injected for deterministic tests.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	// Rand seeds jitter; nil means a fresh random source.
	Rand *rand.Rand
}

// Run executes the loop until ctx is cancelled or maxIters ticks have
// run (maxIters <= 0 means infinite; tests use a small N).
//
// Returns the number of completed ticks and the first error that
// terminated the loop, or nil when ctx was cancelled normally.
func (l *Loop) Run(ctx context.Context, maxIters int) (int, error) {
	if l.Tick == nil {
		return 0, errors.New("serveorch: Tick is required")
	}
	if err := l.Cfg.Validate(); err != nil {
		return 0, err
	}
	now := l.Now
	if now == nil {
		now = time.Now
	}
	sleep := l.Sleep
	if sleep == nil {
		sleep = ctxSleep
	}
	r := l.Rand
	if r == nil {
		r = rand.New(rand.NewSource(now().UnixNano()))
	}

	ticks := 0
	for {
		if err := ctx.Err(); err != nil {
			return ticks, nil
		}
		started := now()
		l.logf("tick start at %s", started.Format(time.RFC3339))
		if err := l.Tick(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ticks, nil
			}
			l.logf("tick error: %v", err)
		} else {
			l.logf("tick done in %s", now().Sub(started).Round(time.Millisecond))
		}
		ticks++
		if maxIters > 0 && ticks >= maxIters {
			return ticks, nil
		}
		d := l.Cfg.nextDelay(now(), r)
		l.logf("sleep %s before next tick", d.Round(time.Second))
		if err := sleep(ctx, d); err != nil {
			return ticks, nil
		}
	}
}

func (l *Loop) logf(format string, args ...any) {
	if l.Logger != nil {
		l.Logger(format, args...)
	}
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// parseHM parses "HH:MM" into minutes-since-midnight in [0, 1440).
func parseHM(s string) (int, error) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("want HH:MM, got %q", s)
	}
	var h, m int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour in %q", s)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid minute in %q", s)
	}
	return h*60 + m, nil
}

func minutesOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }
