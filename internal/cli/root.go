// Package cli wires the cobra command tree.
//
// The root command exposes only the version flag and global flags shared
// by subcommands; subcommands themselves live in sibling files.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the CLI version, stamped at release time via -ldflags.
var Version = "dev"

// NewRootCmd returns the root cobra command for fit-agent.
func NewRootCmd() *cobra.Command {
	var showVersion bool

	cmd := &cobra.Command{
		Use:   "fit-agent",
		Short: "Bridge an AI agent and intervals.icu",
		Long: `fit-agent is a Go CLI that maintains a markdown/YAML workspace
synced with intervals.icu so an AI agent can act as your fitness coach.

The CLI is invoked by an agent; it never calls an LLM itself.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				fmt.Fprintln(cmd.OutOrStdout(), Version)
				return nil
			}
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().String("profile", "", "configuration profile to use (default: from FIT_AGENT_PROFILE, workspace, or 'default')")
	cmd.PersistentFlags().Bool("dry-run", false, "print actions without writing or calling intervals.icu")
	cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	cmd.PersistentFlags().BoolP("quiet", "q", false, "suppress non-error output")

	cmd.Flags().BoolVar(&showVersion, "version", false, "print version and exit")

	cmd.AddCommand(newFitCmd())

	return cmd
}
