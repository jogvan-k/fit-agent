// Package cli — `fit-agent fetch` command.
//
// fetch is the convenience wrapper agents and humans run most often:
// it runs `cache all` followed by `render all` over the same range.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/cache"
	"github.com/jogvan-k/fit-agent/internal/renderorch"
)

func newFetchCmd() *cobra.Command {
	rf := &rangeFlags{}
	var force bool
	var jsonMode bool
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
				if jsonMode {
					return machineResult(cmd, nil, err)
				}
				return err
			}
			rng, err := rf.parse(r.Location)
			if err != nil {
				if jsonMode {
					return machineResult(cmd, nil, err)
				}
				return err
			}
			cctx := cacheContextFrom(cmd, r, dryRun, force)
			// Cache orchestration preserves partial success via Stats.Errors.
			// Retain its typed error arguments for safe machine-layer diagnosis;
			// never parse or emit presentation text or private payloads.
			var cacheErr error
			if jsonMode {
				cctx.Logger = func(_ string, args ...any) {
					for _, arg := range args {
						if e, ok := arg.(error); ok && cacheErr == nil {
							cacheErr = e
						}
					}
				}
			}
			cs, err := cache.All(ctxOrBackground(cmd), cctx, rng)
			if err != nil {
				if jsonMode {
					return machineResult(cmd, nil, err)
				}
				return err
			}
			if !jsonMode {
				printSummary(stdoutOrStderrForResults(cmd), "cache all ["+rng.Oldest+".."+rng.Newest+"]", cs)
			}
			if jsonMode && cs.Errors > 0 {
				if cacheErr == nil {
					cacheErr = fmt.Errorf("fetch completed with errors")
				}
				return machineResult(cmd, nil, cacheErr)
			}

			rctx := renderCtx(cmd, r, dryRun)
			rs, err := renderorch.All(ctxOrBackground(cmd), rctx, rng)
			if jsonMode {
				if err == nil && (cs.Errors > 0 || rs.Errors > 0) {
					err = fmt.Errorf("fetch completed with errors")
				}
				return machineResult(cmd, map[string]any{"from": rng.Oldest, "to": rng.Newest, "cache": cs, "render": rs}, err)
			}
			if err != nil {
				return err
			}
			printSummary(stdoutOrStderrForResults(cmd), "render all ["+rng.Oldest+".."+rng.Newest+"]", rs)
			return nil
		},
	}
	rf.bind(cmd)
	cmd.Flags().BoolVar(&force, "force-refit", false, "re-download FIT files already cached")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "versioned, content-free fetch summary")
	return cmd
}
