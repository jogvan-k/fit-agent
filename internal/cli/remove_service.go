// Package cli — `fit-agent remove-service` command.
//
// remove-service stops, disables, and removes the systemd user unit
// previously installed by setup-service.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/systemdunit"
)

func newRemoveServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-service",
		Short: "Stop, disable, and remove the fit-agent serve systemd user unit",
		Long: `remove-service runs:

    systemctl --user disable --now fit-agent.service
    rm ~/.config/systemd/user/fit-agent.service
    systemctl --user daemon-reload

Errors from systemctl are reported but do not abort the cleanup; the
unit file is removed even when the unit was already inactive.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			dryRun, _ := root.PersistentFlags().GetBool("dry-run")
			errOut := cmd.ErrOrStderr()

			path, err := systemdunit.UnitPath()
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintln(errOut, "would run: systemctl --user disable --now fit-agent.service")
				fmt.Fprintf(errOut, "would remove: %s\n", path)
				fmt.Fprintln(errOut, "would run: systemctl --user daemon-reload")
				return nil
			}

			// Best-effort disable; ignore non-zero exits because the unit
			// may already be inactive or never enabled.
			if !quiet(cmd) {
				fmt.Fprintln(errOut, "systemctl --user disable --now fit-agent.service")
			}
			if out, sErr := systemdunit.RealSystemctl("disable", "--now", systemdunit.UnitName); sErr != nil {
				if !quiet(cmd) {
					if len(out) > 0 {
						_, _ = errOut.Write(out)
					}
					fmt.Fprintf(errOut, "(disable failed: %v; continuing)\n", sErr)
				}
			}

			removed, err := systemdunit.Uninstall()
			if err != nil {
				return err
			}
			if !quiet(cmd) {
				if removed {
					fmt.Fprintf(errOut, "removed %s\n", path)
				} else {
					fmt.Fprintf(errOut, "no unit file at %s\n", path)
				}
			}

			if removed {
				if err := runSystemctl(cmd, "daemon-reload"); err != nil {
					if !quiet(cmd) {
						fmt.Fprintf(errOut, "(daemon-reload failed: %v)\n", err)
					}
				}
			}
			return nil
		},
	}
	return cmd
}
