// Package cli — `fit-agent serve` command.
//
// serve runs fetch (cache all + render all) on a polling cadence,
// designed to stay well inside intervals.icu's rate limits while
// keeping the workspace fresh enough for an agent to act on. See
// internal/serveorch for the loop semantics.
package cli

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/cache"
	"github.com/jogvan-k/fit-agent/internal/renderorch"
	"github.com/jogvan-k/fit-agent/internal/serveorch"
)

func newServeCmd() *cobra.Command {
	var (
		interval      time.Duration
		quietInterval time.Duration
		quietHours    string
		since         string
		jitter        time.Duration
		once          bool
		force         bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run fetch on a polling cadence (daemon mode)",
		Long: `serve polls intervals.icu on a fixed cadence and refreshes the
workspace by running 'cache all' followed by 'render all' over a
small recent window (--since, default 2d).

Defaults are chosen to stay well inside intervals.icu's rate limits
(30 req/s, 132 req/10s; David recommends staying under 10 req/s):

  --interval        15m   active-hours cadence
  --quiet-interval  2h    cadence inside --quiet-hours (e.g. overnight)
  --quiet-hours     ""    HH:MM-HH:MM in athlete-local time; "" disables
  --since           2d    window passed to cache/render every tick
  --jitter          60s   random extra wait per tick

Pass --once to run a single tick and exit (handy for cron-style
scheduling or smoke tests).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}

			qStart, qEnd, err := parseQuietHours(quietHours)
			if err != nil {
				return err
			}
			cfg := serveorch.Config{
				Interval:      interval,
				QuietInterval: quietInterval,
				QuietStart:    qStart,
				QuietEnd:      qEnd,
				Jitter:        jitter,
				Location:      r.Location,
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			rf := rangeFlags{Since: since}
			tick := func(ctx context.Context) error {
				rng, err := rf.parse(r.Location)
				if err != nil {
					return err
				}
				cctx := cacheContextFrom(cmd, r, dryRun, force)
				cs, err := cache.All(ctx, cctx, rng)
				if err != nil {
					return fmt.Errorf("cache all: %w", err)
				}
				printSummary(stdoutOrStderrForResults(cmd), "cache all ["+rng.Oldest+".."+rng.Newest+"]", cs)

				rctx := renderCtx(cmd, r, dryRun)
				rs, err := renderorch.All(ctx, rctx, rng)
				if err != nil {
					return fmt.Errorf("render all: %w", err)
				}
				printSummary(stdoutOrStderrForResults(cmd), "render all ["+rng.Oldest+".."+rng.Newest+"]", rs)
				return nil
			}

			loop := &serveorch.Loop{
				Cfg:    cfg,
				Tick:   tick,
				Logger: makeLogger(cmd),
			}

			ctx, stop := signal.NotifyContext(ctxOrBackground(cmd), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			max := 0
			if once {
				max = 1
			}
			out := cmd.ErrOrStderr()
			if !quiet(cmd) {
				if once {
					fmt.Fprintln(out, "fit-agent serve: running one tick and exiting")
				} else {
					fmt.Fprintf(out, "fit-agent serve: polling every %s (quiet %s); ctrl-c to stop\n",
						cfg.Interval, formatQuiet(cfg))
				}
			}
			ticks, err := loop.Run(ctx, max)
			if !quiet(cmd) {
				fmt.Fprintf(out, "fit-agent serve: stopped after %d tick(s)\n", ticks)
			}
			return err
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", serveorch.DefaultInterval, "active-hours poll cadence")
	cmd.Flags().DurationVar(&quietInterval, "quiet-interval", serveorch.DefaultQuietInterval, "cadence inside --quiet-hours (0 disables quiet hours)")
	cmd.Flags().StringVar(&quietHours, "quiet-hours", "", `quiet window "HH:MM-HH:MM" in athlete-local time, e.g. "23:00-06:00"`)
	cmd.Flags().StringVar(&since, "since", "2d", "lookback window passed to cache/render each tick")
	cmd.Flags().DurationVar(&jitter, "jitter", serveorch.DefaultJitter, "random extra wait added per tick")
	cmd.Flags().BoolVar(&once, "once", false, "run a single tick and exit")
	cmd.Flags().BoolVar(&force, "force-refit", false, "re-download FIT files already cached")
	return cmd
}

// parseQuietHours splits "HH:MM-HH:MM" into (start, end). Empty input
// yields ("",""). Any other malformed value is rejected here; the
// HH:MM portions themselves are validated by serveorch.Config.
func parseQuietHours(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", nil
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf(`--quiet-hours: want "HH:MM-HH:MM", got %q`, s)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

// formatQuiet renders a short human-readable description of the
// quiet-hours configuration for the startup banner.
func formatQuiet(c serveorch.Config) string {
	if c.QuietStart == "" || c.QuietInterval == 0 {
		return "disabled"
	}
	return fmt.Sprintf("%s in %s-%s", c.QuietInterval, c.QuietStart, c.QuietEnd)
}
