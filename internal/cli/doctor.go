package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
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
				r.skip("gh", "the config")
				r.skip("gh login", "the config")
				r.skip("gh user", "the config")
				return r.result()
			}

			gh := func(args ...string) ([]byte, error) {
				return d.Runner.Run(ctx, append(slices.Clone(settings.Command), args...), envPairs(settings.Env))
			}
			out, err := gh("--version")
			if err != nil {
				r.fail("gh", err.Error(), "install gh from https://cli.github.com, or fix gh_command")
				r.skip("gh login", "gh")
				r.skip("gh user", "gh")
				return r.result()
			}
			r.ok("gh", firstLine(out))

			if _, err := gh("auth", "status"); err != nil {
				r.fail("gh login", err.Error(), "log in the account prledger uses: "+loginCommand(settings))
				r.skip("gh user", "gh login")
				return r.result()
			}
			r.ok("gh login", "logged in")

			login, err := gh("api", "user", "--jq", ".login")
			if err != nil {
				r.fail("gh user", err.Error(), "check `gh api user` works")
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

// loginCommand is the gh auth login command for the account prledger uses
// in this repo, environment included.
func loginCommand(s config.Settings) string {
	parts := envPairs(s.Env)
	parts = append(parts, s.Command...)
	return strings.Join(append(parts, "auth", "login"), " ")
}

func envPairs(env map[string]string) []string {
	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	slices.Sort(pairs)
	return pairs
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
