// Package cli wires prledger's cobra commands to their dependencies.
// Commands receive everything they touch through Deps, so tests run them
// in-process with fakes.
package cli

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/prledger/internal/core"
	"github.com/sifatulrabbi/prledger/internal/gh"
)

// Deps holds what the commands need from the outside world.
type Deps struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
	Now     func() time.Time
	// Runner starts gh (and only gh); tests replace it with a fake process.
	Runner gh.Runner
	// DetectRepo finds the repo of the current directory.
	DetectRepo func(ctx context.Context) (core.Repo, error)
}

// globalFlags are flags every command shares.
type globalFlags struct {
	repo   string
	author string
	limit  int
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

	g := &globalFlags{}
	pf := root.PersistentFlags()
	pf.StringVar(&g.repo, "repo", "", "repo as owner/name (default: the origin remote of the current directory)")
	pf.StringVar(&g.author, "author", "@me", "GitHub login whose pull requests to show")
	pf.IntVar(&g.limit, "limit", 1000, "maximum number of pull requests to fetch")

	root.AddCommand(
		&cobra.Command{
			Use:   "version",
			Short: "Print the prledger version",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "prledger %s\n", d.Version)
				return err
			},
		},
		newListCmd(d, g),
	)
	return root
}

// snapshot resolves the repo and builds a Snapshot through gh.
func snapshot(cmd *cobra.Command, d Deps, g *globalFlags) (core.Snapshot, error) {
	ctx := cmd.Context()
	repo, err := resolveRepo(ctx, d, g.repo)
	if err != nil {
		return core.Snapshot{}, err
	}
	ledger := core.Ledger{
		Source: gh.Client{Runner: d.Runner, Command: []string{"gh"}},
		Now:    d.Now,
	}
	return ledger.Snapshot(ctx, core.Query{Repo: repo, Author: g.author, Limit: g.limit})
}

func resolveRepo(ctx context.Context, d Deps, flag string) (core.Repo, error) {
	if flag == "" {
		return d.DetectRepo(ctx)
	}
	owner, name, ok := strings.Cut(flag, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return core.Repo{}, fmt.Errorf("--repo %q: want owner/name", flag)
	}
	return core.Repo{Owner: owner, Name: name}, nil
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
