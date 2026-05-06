// Package cli — `fit-agent fetch` command.
//
// fetch is the convenience wrapper agents and humans run most often:
// it runs `cache all` followed by `render all` over the same range.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/cache"
	"github.com/jogvan-k/fit-agent/internal/renderorch"
)

func newFetchCmd() *cobra.Command {
	rf := &rangeFlags{}
	var force bool
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Cache all + render all (the everyday command)",
		Long: `fetch is the convenience wrapper agents run most often.

It is exactly equivalent to:

    fit-agent cache all  [range flags] [--force-refit]
    fit-agent render all [range flags]

Range defaults to the last 30 days.`,
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
			cs, err := cache.All(ctxOrBackground(cmd), cctx, rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "cache all ["+rng.Oldest+".."+rng.Newest+"]", cs)

			rctx := renderCtx(cmd, r, dryRun)
			rs, err := renderorch.All(ctxOrBackground(cmd), rctx, rng)
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "render all ["+rng.Oldest+".."+rng.Newest+"]", rs)
			return nil
		},
	}
	rf.bind(cmd)
	cmd.Flags().BoolVar(&force, "force-refit", false, "re-download FIT files already cached")
	return cmd
}
