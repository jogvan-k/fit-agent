// Command fit-agent is the CLI entry point.
//
// It is a thin shell that delegates to subcommands assembled in
// internal/cli. The CLI never calls an LLM; it is invoked by an agent.
package main

import (
	"fmt"
	"os"

	"github.com/jogvan-k/fit-agent/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
