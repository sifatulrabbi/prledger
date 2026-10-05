# prledger

See every pull request you've opened in a repo, grouped by feature, with its current status.

Run `prledger` inside any git checkout. It asks the [GitHub CLI](https://cli.github.com) (`gh`) for your pull requests, groups the ones that belong to the same piece of work, and shows them as a table, as JSON, or as a page in your browser. prledger never handles GitHub tokens: whatever account `gh` is logged in as is the account prledger uses.

## Install

```sh
go install github.com/sifatulrabbi/prledger@latest
```

You need `gh` installed and logged in (`gh auth login`). Go 1.27 or newer is needed to build from source.

## Quick start

```sh
cd path/to/your/repo
prledger serve           # opens your PRs in the browser
prledger list            # table of your PRs, grouped
prledger list --json     # the same data as JSON
```

prledger finds the repo from the `origin` remote of the current folder. To look at another repo, pass `--repo owner/name`.

## Commands

| Command | What it does |
| --- | --- |
| `prledger serve` | Opens a page in your browser with every pull request as a card, grouped, with status filters and search. Click a card to open the PR on GitHub. `--port n` picks the port, `--no-open` skips opening the browser. Press Ctrl-C to stop. |
| `prledger list` | Prints your pull requests grouped by feature. `--json` prints the snapshot as JSON. `--cached` shows the last fetched data without calling `gh`. |
| `prledger export html` | Writes the same page as one standalone file you can open or share, with the data baked in. Default file: `prledger-<owner>-<repo>.html` in the current folder; `-o file` picks another. `--cached` uses the last fetched data. |
| `prledger config path` | Prints the config file prledger reads, and says if it does not exist yet. |
| `prledger config init` | Writes a commented starter config. Never overwrites an existing file. |
| `prledger version` | Prints the version. |

Flags that work on every command:

| Flag | Meaning |
| --- | --- |
| `--repo owner/name` | Use this repo instead of the current folder's `origin` remote. |
| `--author login` | Show someone else's pull requests. Default `@me` (the `gh` user). |
| `--limit n` | Fetch at most `n` pull requests. Default 1000. |
| `--config file` | Read this config file. |

## Configuration

prledger works without a config file. Add one when you want to change how `gh` runs for a repo, or to name your own groups.

The file lives at `~/.config/prledger/config.yaml` (or `$XDG_CONFIG_HOME/prledger/config.yaml`). Set `PRLEDGER_CONFIG` or pass `--config` to use another path. `prledger config init` creates it.

```yaml
defaults:
  author: "@me"
  limit: 1000

repos:
  octo/hello-world:
    # Use a second gh account for this repo.
    gh_command: env GH_CONFIG_DIR=~/.config/gh-personal gh
    groups:
      - name: Search
        branch: "search"          # regex on the branch name
      - name: Login fixes
        prs: [12, 15]             # exact PR numbers
      - name: Docs
        title: "(?i)^docs"        # regex on the title
```

Settings in a `repos` section override `defaults`. Repo keys are `owner/name` and are not case-sensitive.

| Key | Meaning |
| --- | --- |
| `gh_command` | How to start `gh`. A string split on spaces, or a list for arguments that contain spaces. Default `gh`. |
| `gh_env` | Extra environment variables for `gh`, as a map. |
| `author` | Whose pull requests to show. Default `@me`. |
| `limit` | Most pull requests to fetch. Default 1000. |
| `auto_groups` | Group pull requests automatically (see below). Default `true`. |
| `ticket_prefixes` | Your issue tracker's key prefixes, e.g. `[ABC, OPS]`. When set, only these count as ticket keys, in any case and with any number of digits. |
| `groups` | Your own groups, in a repo section only. Each has a `name` and at least one of `prs`, `branch`, `title`. |

`~/` and `$VARS` are expanded in `gh_command` and `gh_env`.

### Using a different gh account per repo

`gh_command` runs directly, not through your shell, so shell aliases and functions do not work there. To point `gh` at another account's login, set its config folder in either of these ways:

```yaml
repos:
  me/side-project:
    gh_command: env GH_CONFIG_DIR=~/.config/gh-personal gh
  me/other-project:
    gh_env:
      GH_CONFIG_DIR: ~/.config/gh-personal
```

To log that account in once: `GH_CONFIG_DIR=~/.config/gh-personal gh auth login`.

## The browser page

`prledger serve` listens on `127.0.0.1` only and answers only requests addressed to localhost. The page shows the last fetched data right away and refreshes it in the background when it is more than a minute old. Use the **Refresh** button for the latest status. If a refresh fails (for example `gh` is logged out), the page keeps the data it has and shows the error.

Fetched data is cached in `~/.cache/prledger` (or `$XDG_CACHE_HOME/prledger`), one file per repo and author. Deleting it is safe.

## How grouping works

Each pull request lands in exactly one group, decided in this order:

1. A group in your config that lists the PR's number.
2. The first group in your config whose `branch` or `title` pattern matches.
3. Automatic grouping, if `auto_groups` is on. PRs are linked when they share a ticket key or a branch:
   - A ticket key such as `ABC-123` at the start of a branch path segment (`alice/abc-123-search`, `feature/ABC-123/ui`), or in upper case in the title (`fix: crash (ABC-123)`). Keys need at least two digits, and common words such as `SHA-256`, `UTF-16` or `fix-500` are skipped. If your tracker's prefixes are known, set `ticket_prefixes` and prledger stops guessing.
   - The same branch, or a split of it: `topic-split/part-1` joins `topic`.
   - Links chain: if A shares a key with B and B shares a branch with C, all three are one group. A group is named after its oldest PR.
4. Everything else goes to **Ungrouped**, including automatic groups of a single PR.

Groups are listed with the most recent work first; Ungrouped comes last.

## License

MIT
