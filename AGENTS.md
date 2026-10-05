# prledger — agent notes

Go 1.27 CLI (cobra) that lists the current `gh` user's PRs in the current repo, grouped by feature, and shows them as JSON, a table, or a local HTML page. `README.md` is for humans; this file is for agents working on the code.

## Rules

- All GitHub access goes through the `gh` CLI. Never add token handling, OAuth, or direct GitHub API clients.
- `gh_command` is exec'd as a plain argv, never through a shell. Shell aliases and functions cannot work there; `env VAR=value gh` and the `gh_env` map are the supported ways to change gh's environment.
- Commands get their dependencies through `cli.Deps`; tests run commands in-process with fakes. Fake only things outside the process: the `gh` process (`Deps.Runner`), environment (`Deps.Getenv`), clock and browser. Files are not faked; tests use real temp dirs (`t.TempDir()`), and the CLI harness points `PRLEDGER_CONFIG` at one so a test never reads the real config.
- The Snapshot JSON (`core.Snapshot`) is the one contract shared by `list --json`, the HTTP API and the HTML export. It is pinned by `internal/cli/testdata/list.golden.json`; regenerate with `go test ./internal/cli -update` and review the diff. Bump `core.SchemaVersion` on any change that is not purely additive.
- Ticket-key guessing (`internal/core/grouping.go`) trades recall for precision: keys need 2+ digits, must start a branch path segment or be upper case in a title, and common words (`SHA`, `FIX`, `UTF`, …) are skipped. A repo that sets `ticket_prefixes` skips guessing. Add a regression test with every new false positive.
- Stacks (`internal/core/stack.go`) come before every other rule: a PR's parent is the PR whose head is its base (newest such PR created before it); PRs on a shared base (not the default branch, not a PR head) form one stack per base. Stack groups keep stack order (bottom first, depth-first), never newest-first, and carry `Stack.Entries` parallel to `PRs`. Empty branch names never link (old caches have no base). The default branch comes from `gh repo view` and is stored in the snapshot so cached regrouping can tell shared bases apart.
- Config is validated as a whole at load (every repo section), not just the section in use.
- `serve` security model (`internal/server`): binds 127.0.0.1 only; refuses non-loopback `Host` headers (DNS rebinding); `POST /api/refresh` requires the `X-Prledger` header (cross-site forms cannot send it, and preflights are never granted); every response carries a strict CSP and `nosniff`. Keep all three when adding endpoints; anything that runs gh must be POST.
- `core.Tracker` (serve only) runs fetches under serve's lifetime context, not a caller's: one browser tab leaving must not cancel a fetch others wait on, but Ctrl-C must stop gh. `list`/`export`/`groups suggest` fetch directly under the command context via `snapshotFor`.
- The cache file is keyed by repo, author and a hash of the gh setup (command, `gh_env`, and inherited account variables such as `GH_CONFIG_DIR`/`GH_TOKEN`), because `@me` is a different user per setup. `gh auth switch` inside one config dir is not detected. Cached snapshots are regrouped with the current config on load.
- All gh invocations go through `gh.Client` (`Exec`, `ListPRs`, `CommandLine` for pasteable hints). Do not build gh argv elsewhere.
- Text from PRs (titles, branches) is untrusted: it goes through `textContent` in the page, JSON escaping in exports, and `oneLine` before YAML comments.
- Test fixtures are synthetic. Never commit real PR data from other repos.
- No GitHub workflows. Verify locally with `make check` (gofmt, vet, race tests) before every commit. Releases are Makefile targets (`release-dry`, `release`); see the README's Releasing section. Keep release logic in the Makefile, not in separate scripts.
