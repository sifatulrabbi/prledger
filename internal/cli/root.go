// Package cli wires prledger's cobra commands to their dependencies.
// Commands receive everything they touch through Deps, so tests run them
// in-process with fakes.
package cli

import (
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Deps holds what the commands need from the outside world.
type Deps struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

// NewRoot builds the prledger command tree.
func NewRoot(d Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "prledger",
		Short:         "See every pull request you've opened in a repo, grouped by feature",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the prledger version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "prledger %s\n", d.Version)
			return err
		},
	})
	return root
}

// ResolveVersion picks the version to report. A release build sets it with
// -ldflags "-X main.version=…"; `go install …@vX` leaves that empty but
// records the module version; anything else is a local build.
func ResolveVersion(ldflag string, info *debug.BuildInfo) string {
	if ldflag != "" {
		return ldflag
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
