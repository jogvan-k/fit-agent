package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/jogvan-k/fit-agent/internal/workoutdsl"
	"github.com/spf13/cobra"
)

// newWorkoutCmd returns the `fit-agent workout` subtree: parse, render
// and lint the fit-workout DSL. Pure offline operations; no network or
// workspace I/O.
func newWorkoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workout",
		Short: "Parse, render and lint fit-workout DSL blocks (no network)",
		Long: `workout groups subcommands that operate on the fit-workout DSL
used inside planned-workouts/*.md. Useful for validating a DSL block
before pushing to intervals.icu, or for converting one between the
canonical DSL form and the intervals.icu workout-description string.`,
	}
	cmd.AddCommand(newWorkoutParseCmd(), newWorkoutRenderCmd(), newWorkoutLintCmd())
	return cmd
}

func newWorkoutParseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parse <path|->",
		Short: "Parse a fit-workout block and emit canonical DSL form",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := readDSLSource(cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			w, err := workoutdsl.Parse(src)
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), workoutdsl.RenderDSL(w))
			return err
		},
	}
	return cmd
}

func newWorkoutRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render <path|->",
		Short: "Convert a fit-workout block to the intervals.icu workout-description string",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := readDSLSource(cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			w, err := workoutdsl.Parse(src)
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), workoutdsl.RenderICU(w))
			return err
		},
	}
	return cmd
}

func newWorkoutLintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint <path|->",
		Short: "Validate a fit-workout block and print a one-line summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := readDSLSource(cmd.InOrStdin(), args[0])
			if err != nil {
				return err
			}
			w, err := workoutdsl.Parse(src)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), workoutdsl.Summary(w))
			return nil
		},
	}
	return cmd
}

// readDSLSource reads a DSL source from a file path or "-" for stdin.
func readDSLSource(stdin io.Reader, path string) (string, error) {
	var (
		buf []byte
		err error
	)
	if path == "-" {
		buf, err = io.ReadAll(stdin)
	} else {
		buf, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read dsl: %w", err)
	}
	return string(buf), nil
}
