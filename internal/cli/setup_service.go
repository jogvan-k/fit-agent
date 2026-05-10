// Package cli — `fit-agent setup-service` command.
//
// setup-service generates a systemd user unit at
// ~/.config/systemd/user/fit-agent.service that runs `fit-agent serve`
// in the background, then optionally enables and starts it.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/systemdunit"
)

func newSetupServiceCmd() *cobra.Command {
	var (
		binary    string
		profile   string
		extraArgs []string
		workdir   string
		noEnable  bool
		noStart   bool
		printOnly bool
	)
	cmd := &cobra.Command{
		Use:   "setup-service",
		Short: "Install and enable the fit-agent serve systemd user unit",
		Long: `setup-service writes ~/.config/systemd/user/fit-agent.service
and (by default) runs:

    systemctl --user daemon-reload
    systemctl --user enable --now fit-agent.service

The unit invokes 'fit-agent serve' on the polling cadence chosen via
the flags below. To inspect the rendered unit without writing it, use
--print. To install without activating, pass --no-enable and/or --no-start.

The unit lives in your per-user systemd instance, so no root is
required and the daemon shuts down with your session unless you
'loginctl enable-linger <user>'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			dryRun, _ := root.PersistentFlags().GetBool("dry-run")

			if binary == "" {
				exe, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolve current executable (pass --binary): %w", err)
				}
				binary = exe
			}
			if profile == "" {
				profile, _ = root.PersistentFlags().GetString("profile")
			}

			p := systemdunit.Params{
				Binary:     binary,
				Profile:    profile,
				ExtraArgs:  extraArgs,
				WorkingDir: workdir,
			}
			if err := p.Validate(); err != nil {
				return err
			}

			body, err := systemdunit.RenderString(p)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()

			if printOnly {
				fmt.Fprint(out, body)
				return nil
			}

			path, err := systemdunit.UnitPath()
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintf(errOut, "would write %s:\n%s", path, body)
				if !noEnable {
					fmt.Fprintln(errOut, "would run: systemctl --user daemon-reload")
					fmt.Fprintf(errOut, "would run: systemctl --user enable%s fit-agent.service\n", startFlag(!noStart))
				}
				return nil
			}

			written, err := systemdunit.Install(p)
			if err != nil {
				return err
			}
			if !quiet(cmd) {
				fmt.Fprintf(errOut, "wrote %s\n", written)
			}

			if noEnable && noStart {
				if !quiet(cmd) {
					fmt.Fprintln(errOut, "skipped daemon-reload/enable/start; run them manually if needed")
				}
				return nil
			}

			if err := runSystemctl(cmd, "daemon-reload"); err != nil {
				return err
			}
			if !noEnable {
				args := []string{"enable"}
				if !noStart {
					args = append(args, "--now")
				}
				args = append(args, systemdunit.UnitName)
				if err := runSystemctl(cmd, args...); err != nil {
					return err
				}
			} else if !noStart {
				if err := runSystemctl(cmd, "start", systemdunit.UnitName); err != nil {
					return err
				}
			}
			if !quiet(cmd) {
				fmt.Fprintln(errOut, "fit-agent.service installed; check 'systemctl --user status fit-agent.service'")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&binary, "binary", "", "absolute path to fit-agent (default: current executable)")
	cmd.Flags().StringVar(&profile, "profile-name", "", "configuration profile to bake into the unit (default: --profile, then 'default')")
	cmd.Flags().StringSliceVar(&extraArgs, "serve-arg", nil, "extra flag(s) to pass to 'serve'; repeatable, e.g. --serve-arg=--quiet-hours --serve-arg=23:00-06:00")
	cmd.Flags().StringVar(&workdir, "working-dir", "", "WorkingDirectory= for the unit (optional)")
	cmd.Flags().BoolVar(&noEnable, "no-enable", false, "do not run 'systemctl --user enable'")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "do not start the unit (only relevant without --no-enable)")
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the rendered unit and exit; do not touch disk")
	return cmd
}

func startFlag(start bool) string {
	if start {
		return " --now"
	}
	return ""
}

// runSystemctl executes systemctl --user <args...> and streams output
// through the cobra command. Errors include the combined output for
// debuggability.
func runSystemctl(cmd *cobra.Command, args ...string) error {
	if !quiet(cmd) {
		fmt.Fprintf(cmd.ErrOrStderr(), "systemctl --user %s\n", strings.Join(args, " "))
	}
	out, err := systemdunit.RealSystemctl(args...)
	if len(out) > 0 && !quiet(cmd) {
		_, _ = cmd.ErrOrStderr().Write(out)
	}
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
