# prledger — agent notes

Go 1.27 CLI (cobra) that lists the current `gh` user's PRs in the current repo, grouped by feature, and shows them as JSON, a table, or a local HTML page. `README.md` is for humans; this file is for agents working on the code.

## Rules

- All GitHub access goes through the `gh` CLI. Never add token handling, OAuth, or direct GitHub API clients.
- `gh_command` is exec'd as a plain argv, never through a shell. Shell aliases and functions cannot work there; `env VAR=value gh` and the `gh_env` map are the supported ways to change gh's environment.
- Commands get their dependencies through `cli.Deps`; tests run commands in-process with fakes. Fake only things outside the process (the `gh` process, files, clock, browser).
- Test fixtures are synthetic. Never commit real PR data from other repos.
- No GitHub workflows. Verify locally: `go vet ./... && go test ./...`.

## Developing in this repo

The maintainer has two GitHub accounts. For `gh` and `git` in this repo use the personal one: prefix `gh` with `GH_CONFIG_DIR=$HOME/.config/gh-personal` (the `with-gh-personal` alias in an interactive shell). The `origin` remote uses the `personal.github.com` SSH host alias; plain `github.com` authenticates as the work account.
