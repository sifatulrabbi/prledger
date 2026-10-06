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
		newGroupsCmd(d, g),
		newDoctorCmd(d, g),
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

func (t target) client(d Deps) gh.Client {
	return clientFor(d, t.settings)
}

func clientFor(d Deps, s config.Settings) gh.Client {
	return gh.Client{Runner: d.Runner, Command: s.Command, Env: s.Env}
}

func (t target) ledger(d Deps) core.Ledger {
	return core.Ledger{
		Source: t.client(d),
		Now:    d.Now,
		Grouping: core.Grouping{
			Rules:          t.settings.Groups,
			Auto:           t.settings.AutoGroups,
			TicketPrefixes: t.settings.TicketPrefixes,
		},
	}
}

// ghAccountVars are inherited environment variables that change which
// account gh acts as.
var ghAccountVars = []string{"GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// account identifies the gh setup: its command, its configured env, and the
// account variables it inherits. Only a hash of it is stored.
func (t target) account(d Deps) []string {
	id := append(gh.EnvList(t.settings.Env), t.settings.Command...)
	for _, k := range ghAccountVars {
		if _, set := t.settings.Env[k]; !set {
			if v := d.Getenv(k); v != "" {
				id = append(id, "inherited:"+k+"="+v)
			}
		}
	}
	return id
}

func (t target) query() core.Query {
	return core.Query{Repo: t.repo, Author: t.settings.Author, Limit: t.settings.Limit}
}

// cache is the on-disk snapshot cache for this target. Loaded snapshots are
// regrouped with the current config, which may have changed since they were
// saved.
func (t target) cache(d Deps) (regroupedCache, error) {
	file, err := cache.For(d.Getenv, t.repo, t.settings.Author, t.account(d))
	if err != nil {
		return regroupedCache{}, err
	}
	return regroupedCache{File: file, ledger: t.ledger(d)}, nil
}

// tracker fetches through gh and keeps the cache up to date until life ends.
func (t target) tracker(life context.Context, d Deps) (*core.Tracker, error) {
	c, err := t.cache(d)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context) (core.Snapshot, error) { return c.ledger.Snapshot(ctx, t.query()) }
	return core.NewTracker(life, fetch, c), nil
}

// snapshotFor returns the cached snapshot, or fetches a fresh one through gh
// and saves it to the cache.
func snapshotFor(cmd *cobra.Command, t target, d Deps, cached bool) (core.Snapshot, error) {
	c, err := t.cache(d)
	if err != nil {
		return core.Snapshot{}, err
	}
	if cached {
		snap, ok, err := c.Load()
		if err != nil {
			return core.Snapshot{}, fmt.Errorf("%w (run without --cached to rebuild it)", err)
		}
		if !ok {
			return core.Snapshot{}, fmt.Errorf("no cached snapshot for %s yet; run without --cached first", t.repo)
		}
		return snap, nil
	}
	snap, err := c.ledger.Snapshot(cmd.Context(), t.query())
	if err != nil {
		return core.Snapshot{}, err
	}
	for _, w := range snap.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
	}
	if err := c.Save(snap); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update the cache: %v\n", err)
	}
	return snap, nil
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
