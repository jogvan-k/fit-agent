package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/pushorch"
)

func newPushWorkoutsCmd() *cobra.Command {
	var (
		rf    rangeFlags
		prune bool
	)
	cmd := &cobra.Command{
		Use:   "push-workouts",
		Short: "Deprecated: push-only half of sync-workouts",
		Long: `push-workouts is retained for backward compatibility and runs the
push half of ` + "`sync-workouts`" + ` only: it diffs every
planned-workouts/*.md file against the cached .cache/events/*.json
snapshot and applies create / update / delete actions to intervals.icu.

Prefer ` + "`fit-agent sync-workouts`" + `, which additionally pulls
events authored on intervals.icu (or by other devices) and refreshes
the machine-owned YAML block inside each per-day planned-workout
markdown file so the agent can see them.

Run ` + "`fit-agent cache events`" + ` (or ` + "`fit-agent fetch`" + `) first so that
the diff sees the current calendar state. Server-assigned event IDs are
stamped back into the markdown frontmatter (icu_event_id) so subsequent
pushes can issue PUT updates rather than recreate.

By default events that exist on intervals.icu but no longer appear in
the markdown are reported as 'skip' (cache-only event). Pass --prune to
DELETE them.`,
		Deprecated: "use `fit-agent sync-workouts` instead (it runs push then pull).",
		RunE: func(cmd *cobra.Command, args []string) error {
			res, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			r, err := rf.parse(res.Location)
			if err != nil {
				return err
			}
			pctx := pushorch.Context{
				Client:    res.Client,
				AthleteID: res.Profile.IcuAthleteID,
				Layout:    res.Layout,
				Location:  res.Location,
				DryRun:    dryRun,
				Prune:     prune,
				Logger:    makeLogger(cmd),
			}
			actions, err := pushorch.Plan(ctxOrBackground(cmd), pctx, r)
			if err != nil {
				return err
			}
			out := stdoutOrStderrForResults(cmd)
			if !quiet(cmd) {
				for _, a := range actions {
					reason := a.Reason
					if reason == "" {
						reason = "-"
					}
					fmt.Fprintf(out, "%-9s %s %-28s %s\n", a.Kind, a.Date, truncName(a.Name, 28), reason)
				}
			}
			if err := pushorch.Apply(ctxOrBackground(cmd), pctx, actions); err != nil {
				return err
			}
			printSummary(out, "push-workouts", pushorch.Summarise(actions))
			return nil
		},
	}
	rf.bind(cmd)
	cmd.Flags().BoolVar(&prune, "prune", false, "DELETE icu events that no longer appear in markdown")
	return cmd
}

func truncName(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
