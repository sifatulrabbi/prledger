package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/prledger/internal/config"
	"github.com/sifatulrabbi/prledger/internal/core"
)

func newDoctorCmd(d Deps, g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that prledger can find your repo, config and gh login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			r := &report{w: cmd.OutOrStdout()}

			repo, err := resolveRepo(ctx, d, g.repo)
			if err != nil {
				r.fail("repo", err.Error(), "run inside a git checkout with an origin remote, or pass --repo owner/name")
			} else {
				r.ok("repo", repo.String())
			}

			settings, err := doctorConfig(d, g, repo, r)
			if err != nil {
				r.skip("worktrees", "the config")
				r.skip("gh", "the config")
				r.skip("gh login", "the config")
				r.skip("gh user", "the config")
				return r.result()
			}

			switch {
			case !settings.WorktreeGroups:
				r.ok("worktrees", "not used (worktree_groups: false)")
			case repo == (core.Repo{}):
				r.skip("worktrees", "repo")
			case !isLocal(ctx, d, g.repo, repo):
				r.ok("worktrees", "not used: this folder is not a checkout of "+repo.String())
			default:
				// Reading worktrees only improves grouping, so a failure is a
				// note rather than a failed check.
				if wts, err := d.Worktrees(ctx); err != nil {
					r.line("note", "worktrees", "not grouping by worktree: "+err.Error())
				} else {
					r.ok("worktrees", fmt.Sprintf("%d found (the main checkout is not counted)", len(wts)))
				}
			}

			client := clientFor(d, settings)
			out, err := client.Exec(ctx, "--version")
			if err != nil {
				r.fail("gh", err.Error(), "install gh from https://cli.github.com, or fix gh_command")
				r.skip("gh login", "gh")
				r.skip("gh user", "gh")
				return r.result()
			}
			r.ok("gh", firstLine(out))

			if _, err := client.Exec(ctx, "auth", "status", "--active"); err != nil {
				r.fail("gh login", err.Error(), "log in the account prledger uses: "+client.CommandLine("auth", "login"))
				r.skip("gh user", "gh login")
				return r.result()
			}
			r.ok("gh login", "logged in")

			login, err := client.Exec(ctx, "api", "user", "--jq", ".login")
			if err != nil {
				r.fail("gh user", err.Error(), "see what gh says: "+client.CommandLine("api", "user"))
			} else {
				r.ok("gh user", "acting as "+firstLine(login))
			}
			return r.result()
		},
	}
}

// doctorConfig reports on the config file and returns the resolved settings.
// With no repo detected it resolves the defaults section.
func doctorConfig(d Deps, g *globalFlags, repo core.Repo, r *report) (config.Settings, error) {
	path, err := configPath(d, g)
	if err != nil {
		r.fail("config", err.Error(), "set PRLEDGER_CONFIG or pass --config")
		return config.Settings{}, err
	}
	file, err := config.Load(path)
	if err == nil {
		var s config.Settings
		if s, err = file.For(repo, d.Getenv); err == nil {
			if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
				r.ok("config", path+" (not created; using built-in defaults)")
			} else {
				r.ok("config", path)
			}
			return s, nil
		}
	}
	r.fail("config", err.Error(), "fix the file, or move it aside to use the built-in defaults")
	return config.Settings{}, err
}

func firstLine(b []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return line
}

type report struct {
	w      io.Writer
	failed int
}

func (r *report) line(state, check, detail string) {
	fmt.Fprintf(r.w, "%-5s %-9s %s\n", state, check, detail)
}

func (r *report) ok(check, detail string) { r.line("ok", check, detail) }

func (r *report) fail(check, detail, hint string) {
	r.failed++
	r.line("FAIL", check, detail)
	fmt.Fprintf(r.w, "%16s→ %s\n", "", hint)
}

func (r *report) skip(check, after string) { r.line("skip", check, "skipped until "+after+" passes") }

func (r *report) result() error {
	if r.failed == 0 {
		return nil
	}
	return fmt.Errorf("%d check(s) failed", r.failed)
}
