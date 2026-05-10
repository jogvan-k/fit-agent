package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/cache"
	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/runtime"
)

// rangeFlags holds the --since/--from/--to values shared by every
// range-aware subcommand.
type rangeFlags struct {
	Since string
	From  string
	To    string
}

func (rf *rangeFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&rf.Since, "since", "", "lookback window: 30d, 12w, 6m (default 30d)")
	cmd.Flags().StringVar(&rf.From, "from", "", "start date YYYY-MM-DD (use with --to)")
	cmd.Flags().StringVar(&rf.To, "to", "", "end date YYYY-MM-DD (use with --from)")
}

func (rf rangeFlags) parse(loc *time.Location) (daterange.Range, error) {
	return daterange.Parse(daterange.Inputs{
		Since: rf.Since, From: rf.From, To: rf.To,
	}, loc, time.Now())
}

// resolveRuntime is the boilerplate every command runs to get a
// *runtime.Resolved + dry-run flag from the cobra command tree.
func resolveRuntime(cmd *cobra.Command) (*runtime.Resolved, bool, error) {
	root := cmd.Root()
	profile, _ := root.PersistentFlags().GetString("profile")
	dryRun, _ := root.PersistentFlags().GetBool("dry-run")
	r, err := runtime.Resolve(cmd.Context(), runtime.Options{ProfileFlag: profile})
	if err != nil {
		return nil, dryRun, err
	}
	return r, dryRun, nil
}

// printSummary writes a one-line summary to out.
func printSummary(out io.Writer, label string, summary fmt.Stringer) {
	fmt.Fprintf(out, "%s: %s\n", label, summary)
}

// quiet returns true when --quiet was passed on the root command.
func quiet(cmd *cobra.Command) bool {
	q, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	return q
}

// makeLogger returns a logger function suitable for cache.Context /
// renderorch.Context that respects --quiet and --verbose.
func makeLogger(cmd *cobra.Command) func(string, ...any) {
	if quiet(cmd) {
		return func(string, ...any) {}
	}
	verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
	out := cmd.ErrOrStderr()
	if !verbose {
		return func(string, ...any) {}
	}
	return func(format string, args ...any) {
		fmt.Fprintf(out, "  "+format+"\n", args...)
	}
}

// cacheContextFrom builds a cache.Context bundle from a resolved
// runtime + cobra command + flags.
func cacheContextFrom(cmd *cobra.Command, r *runtime.Resolved, dryRun, forceRefit bool) cache.Context {
	return cache.Context{
		Client:     r.Client,
		Layout:     r.Layout,
		AthleteID:  r.Profile.IcuAthleteID,
		Location:   r.Location,
		DryRun:     dryRun,
		ForceRefit: forceRefit,
		Logger:     makeLogger(cmd),
	}
}

// ctxOrBackground returns cmd.Context() falling back to context.Background().
func ctxOrBackground(cmd *cobra.Command) context.Context {
	if c := cmd.Context(); c != nil {
		return c
	}
	return context.Background()
}

// stdoutOrStderrForResults picks where progress messages go vs where
// machine-readable summaries go. Today both just use the inherited
// streams; --json output (M9 candidate) will route to stdout.
func stdoutOrStderrForResults(cmd *cobra.Command) io.Writer {
	if quiet(cmd) {
		return io.Discard
	}
	return cmd.OutOrStdout()
}