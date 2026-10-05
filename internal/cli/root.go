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

	"github.com/sifatulrabbi/prledger/internal/cache"
	"github.com/sifatulrabbi/prledger/internal/config"
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
	// Getenv reads environment variables (config path, $HOME, $VAR expansion).
	Getenv func(string) string
	// OpenBrowser opens a URL in the user's browser.
	OpenBrowser func(url string) error
}

// globalFlags are flags every command shares.
type globalFlags struct {
	config string
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
	pf.StringVar(&g.config, "config", "", "config file (default: $PRLEDGER_CONFIG, else ~/.config/prledger/config.yaml)")
	pf.StringVar(&g.repo, "repo", "", "repo as owner/name (default: the origin remote of the current directory)")
	pf.StringVar(&g.author, "author", "", `GitHub login whose pull requests to show (default from config, else "@me")`)
	pf.IntVar(&g.limit, "limit", 0, "maximum number of pull requests to fetch (default from config, else 1000)")

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
		newConfigCmd(d, g),
		newServeCmd(d, g),
		newExportCmd(d, g),
	)
	return root
}

// configPath is the config file in use: --config, else the default lookup.
func configPath(d Deps, g *globalFlags) (string, error) {
	if g.config != "" {
		return g.config, nil
	}
	return config.Path(d.Getenv)
}

// target is a resolved repo plus its settings, with flags applied.
type target struct {
	repo     core.Repo
	settings config.Settings
}

func resolveTarget(cmd *cobra.Command, d Deps, g *globalFlags) (target, error) {
	repo, err := resolveRepo(cmd.Context(), d, g.repo)
	if err != nil {
		return target{}, err
	}
	path, err := configPath(d, g)
	if err != nil {
		return target{}, err
	}
	file, err := config.Load(path)
	if err != nil {
		return target{}, err
	}
	s, err := file.For(repo, d.Getenv)
	if err != nil {
		return target{}, fmt.Errorf("config %s: %w", path, err)
	}
	flags := cmd.Flags()
	if flags.Changed("author") {
		s.Author = g.author
	}
	if flags.Changed("limit") {
		if g.limit < 1 {
			return target{}, fmt.Errorf("--limit must be at least 1, got %d", g.limit)
		}
		s.Limit = g.limit
	}
	return target{repo: repo, settings: s}, nil
}

func (t target) ledger(d Deps) core.Ledger {
	return core.Ledger{
		Source:   gh.Client{Runner: d.Runner, Command: t.settings.Command, Env: t.settings.Env},
		Now:      d.Now,
		Grouping: core.Grouping{Rules: t.settings.Groups, Auto: t.settings.AutoGroups, TicketPrefixes: t.settings.TicketPrefixes},
	}
}

func (t target) query() core.Query {
	return core.Query{Repo: t.repo, Author: t.settings.Author, Limit: t.settings.Limit}
}

// cache is the on-disk snapshot cache for this target. Loaded snapshots are
// regrouped with the current config, which may have changed since they were
// saved.
func (t target) cache(d Deps) (regroupedCache, error) {
	file, err := cache.For(d.Getenv, t.repo, t.settings.Author)
	if err != nil {
		return regroupedCache{}, err
	}
	return regroupedCache{File: file, ledger: t.ledger(d)}, nil
}

// tracker fetches through gh and keeps the cache up to date.
func (t target) tracker(d Deps) (*core.Tracker, error) {
	c, err := t.cache(d)
	if err != nil {
		return nil, err
	}
	l := t.ledger(d)
	fetch := func(ctx context.Context) (core.Snapshot, error) { return l.Snapshot(ctx, t.query()) }
	return core.NewTracker(fetch, c), nil
}

type regroupedCache struct {
	cache.File
	ledger core.Ledger
}

func (c regroupedCache) Load() (core.Snapshot, bool, error) {
	snap, ok, err := c.File.Load()
	if ok {
		snap = c.ledger.Regroup(snap)
	}
	return snap, ok, err
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
