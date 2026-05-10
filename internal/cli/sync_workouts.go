package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/syncorch"
)

func newSyncWorkoutsCmd() *cobra.Command {
	var (
		rf    rangeFlags
		prune bool
	)
	cmd := &cobra.Command{
		Use:   "sync-workouts",
		Short: "Two-way sync of planned workouts with intervals.icu",
		Long: `sync-workouts is the agent's primary workout-calendar command.

It runs in two phases:

  1. Push: every agent-authored fit-agent/planned-workouts/*.md file in
     range is diffed against the cached intervals.icu snapshot and the
     resulting create / update / delete actions are sent to icu (same
     behaviour as the legacy ` + "`push-workouts`" + ` command).

  2. Pull: every WORKOUT-category event in range is fetched fresh from
     icu. Events that are not authored locally (no matching .md file
     by stamped icu_event_id, or by date+name) are materialised as
     read-only mirror files at
     ` + "`fit-agent/planned-workouts/YYYY-MM-DD.<id>.icu.md`" + `.
     Read-only mirrors whose icu event has been deleted are removed.

Push runs first so that workouts the agent just authored are returned
by the subsequent pull and stamped with their server-assigned id in
the locally-authored file.

Pass --prune to also DELETE icu events that no longer have a matching
locally-authored markdown file (otherwise such events are reported as
'skip' during the push step).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, dryRun, err := resolveRuntime(cmd)
			if err != nil {
				return err
			}
			r, err := rf.parse(res.Location)
			if err != nil {
				return err
			}
			sctx := syncorch.Context{
				Client:    res.Client,
				AthleteID: res.Profile.IcuAthleteID,
				Layout:    res.Layout,
				Location:  res.Location,
				DryRun:    dryRun,
				Prune:     prune,
				Logger:    makeLogger(cmd),
			}
			result, err := syncorch.Sync(ctxOrBackground(cmd), sctx, r)
			out := stdoutOrStderrForResults(cmd)
			fmt.Fprintf(out, "sync-workouts: %s\n", result)
			return err
		},
	}
	rf.bind(cmd)
	cmd.Flags().BoolVar(&prune, "prune", false, "DELETE icu events that no longer appear in markdown")
	return cmd
}
