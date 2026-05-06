// Package cli — `fit-agent cache` command group.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/cache"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Sync raw intervals.icu payloads into the workspace .cache/ tree",
		Long: `cache stores the raw intervals.icu JSON (and FIT binaries)
under <workspace>/fit-agent/.cache/.

The cache is the source of truth for everything `+"`render`"+` produces;
agent-facing YAML/markdown is regenerated from it on every fetch.

Subcommands operate on a date range via --since N(d|w|m) (default 30d)
or --from/--to. All commands honour --dry-run.`,
	}
	cmd.AddCommand(
		newCacheAthleteCmd(),
		newCacheActivitiesCmd(),
		newCacheActivityCmd(),
		newCacheWellnessCmd(),
		newCacheEventsCmd(),
		newCacheAllCmd(),
	)
	return cmd
}

func newCacheAthleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "athlete",
		Short: "Cache /athlete/0 → .cache/athlete.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			out, err := cache.Athlete(ctxOrBackground(cmd), cacheContextFrom(cmd, r, dryRun, false))
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutOrStderrForResults(cmd), "athlete: %s\n", out)
			return nil
		},
	}
}

func newCacheActivitiesCmd() *cobra.Command {
	rf := &rangeFlags{}
	var force bool
	cmd := &cobra.Command{
		Use:   "activities",
		Short: "Cache the activity list + per-activity JSON + FIT binaries for a range",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			cctx := cacheContextFrom(cmd, r, dryRun, force)
			s, err := cache.Activities(ctxOrBackground(cmd), cctx, rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "activities ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	cmd.Flags().BoolVar(&force, "force-refit", false, "re-download FIT files already cached")
	return cmd
}

func newCacheActivityCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "activity <id>",
		Short: "Cache one activity's JSON + FIT (e.g. i123456789)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			cctx := cacheContextFrom(cmd, r, dryRun, force)
			s, err := cache.SingleActivity(ctxOrBackground(cmd), cctx, args[0])
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "activity "+args[0], s)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force-refit", false, "re-download FIT file already cached")
	return cmd
}

func newCacheWellnessCmd() *cobra.Command {
	rf := &rangeFlags{}
	cmd := &cobra.Command{
		Use:   "wellness",
		Short: "Cache the daily wellness rows for a range",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			s, err := cache.Wellness(ctxOrBackground(cmd), cacheContextFrom(cmd, r, dryRun, false), rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "wellness ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	return cmd
}

func newCacheEventsCmd() *cobra.Command {
	rf := &rangeFlags{}
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Cache planned-workout events (category=WORKOUT) for a range",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			s, err := cache.Events(ctxOrBackground(cmd), cacheContextFrom(cmd, r, dryRun, false), rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "events ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	return cmd
}

func newCacheAllCmd() *cobra.Command {
	rf := &rangeFlags{}
	var force bool
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Run athlete + activities + wellness + events for a range",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			s, err := cache.All(ctxOrBackground(cmd), cacheContextFrom(cmd, r, dryRun, force), rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "cache all ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	cmd.Flags().BoolVar(&force, "force-refit", false, "re-download FIT files already cached")
	return cmd
}

// (no helper functions; cache.Outcome implements fmt.Stringer.)
