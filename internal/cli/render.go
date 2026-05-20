// Package cli — `fit-agent render` command group.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/renderorch"
	"github.com/jogvan-k/fit-agent/internal/runtime"
)

func newRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Materialise agent-facing YAML/markdown from .cache/",
		Long: `render reads cached intervals.icu payloads and parsed FIT files
and writes the agent-facing files described in agent-plan.md §10.

Subcommands operate on a date range via --since/--from/--to. They are
read-only with respect to .cache/; only files in fit-agent/activities,
fit-agent/wellness, and fit-agent/planned-workouts are written.`,
	}
	cmd.AddCommand(
		newRenderActivitiesCmd(),
		newRenderActivityCmd(),
		newRenderWellnessCmd(),
		newRenderPlannedCmd(),
		newRenderAllCmd(),
	)
	return cmd
}

func newRenderActivitiesCmd() *cobra.Command {
	rf := &rangeFlags{}
	cmd := &cobra.Command{
		Use:   "activities",
		Short: "Render fit-agent/activities/YYYY-MM-DD.yaml for a range",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			s, err := renderorch.Activities(ctxOrBackground(cmd), renderCtx(cmd, r, dryRun), rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "render activities ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	return cmd
}

func newRenderActivityCmd() *cobra.Command {
	var stdout bool
	cmd := &cobra.Command{
		Use:   "activity <id>",
		Short: "Render a single activity (writes the day file; --stdout prints it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			body, out, err := renderorch.SingleActivity(ctxOrBackground(cmd), renderCtx(cmd, r, dryRun || stdout), args[0])
			if err != nil {
				return err
			}
			if stdout {
				_, _ = cmd.OutOrStdout().Write(body)
				return nil
			}
			fmt.Fprintf(stdoutOrStderrForResults(cmd), "render activity %s: %s\n", args[0], out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&stdout, "stdout", false, "print rendered YAML to stdout instead of writing the day file")
	return cmd
}

func newRenderWellnessCmd() *cobra.Command {
	rf := &rangeFlags{}
	cmd := &cobra.Command{
		Use:   "wellness",
		Short: "Render fit-agent/wellness/YYYY-MM.yaml for a range",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			s, err := renderorch.Wellness(ctxOrBackground(cmd), renderCtx(cmd, r, dryRun), rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "render wellness ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	return cmd
}

func newRenderPlannedCmd() *cobra.Command {
	rf := &rangeFlags{}
	cmd := &cobra.Command{
		Use:   "planned",
		Short: "Render fit-agent/planned-workouts/YYYY-MM-DD.md for a range",
		Long:  "Existing markdown files are preserved (shared-ownership).",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			s, err := renderorch.Planned(ctxOrBackground(cmd), renderCtx(cmd, r, dryRun), rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "render planned ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	return cmd
}

func newRenderAllCmd() *cobra.Command {
	rf := &rangeFlags{}
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Render activities + wellness + planned for a range",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				return err
			}
			s, err := renderorch.All(ctxOrBackground(cmd), renderCtx(cmd, r, dryRun), rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "render all ["+rng.Oldest+".."+rng.Newest+"]", s)
			return nil
		},
	}
	rf.bind(cmd)
	return cmd
}

func renderCtx(cmd *cobra.Command, r *runtime.Resolved, dryRun bool) renderorch.Context {
	autoSplitM, enabled := r.Profile.AutoSplitDistanceM()
	if !enabled {
		autoSplitM = -1 // sentinel: user explicitly disabled
	}
	return renderorch.Context{
		Layout:             r.Layout,
		Location:           r.Location,
		DryRun:             dryRun,
		Logger:             makeLogger(cmd),
		AutoSplitDistanceM: autoSplitM,
	}
}
