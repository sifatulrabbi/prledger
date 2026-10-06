# prledger

See every pull request you've opened in a repo, grouped by feature, with its current status.

Run `prledger` inside any git checkout. It asks the [GitHub CLI](https://cli.github.com) (`gh`) for your pull requests, groups the ones that belong to the same piece of work, and shows them as a table, as JSON, or as a page in your browser. prledger never handles GitHub tokens: whatever account `gh` is logged in as is the account prledger uses.

## Install

```sh
go install github.com/sifatulrabbi/prledger@latest
```

Or download an archive for your system from the [releases page](https://github.com/sifatulrabbi/prledger/releases), unpack it and put `prledger` on your `PATH`. `checksums.txt` on the same page lets you verify the download.

You need `gh` installed and logged in (`gh auth login`). Go 1.27 or newer is needed to build from source. If something does not work, run `prledger doctor` first; it says what is wrong and how to fix it.

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
| `prledger serve` | Opens a page in your browser with your pull requests one per line, grouped, showing labels, reviewers, CI checks, merge conflicts and whether each open PR needs you. Has filters and search. Click a PR to open it on GitHub. `--port n` picks the port, `--no-open` skips opening the browser. Press Ctrl-C to stop. |
| `prledger list` | Prints your pull requests grouped by feature, with a short CI, review and merge note for open ones (`ci ✗ · changes requested · conflicts`). `--json` prints the snapshot as JSON. `--cached` shows the last fetched data without calling `gh`. |
| `prledger export html` | Writes the same page as one standalone file you can open or share, with the data baked in. Default file: `prledger-<owner>-<repo>.html` in the current folder; `-o file` picks another. `--cached` uses the last fetched data. |
| `prledger groups suggest` | Prints groups built from the automatic rules for every PR your config does not already group, plus a commented list of PRs it could not link. With no groups configured yet it prints a `groups:` block to paste under the repo; otherwise it prints list items to append under your existing `groups:`. Rename them as you like. `--cached` uses the last fetched data. |
| `prledger doctor` | Checks, one line each, that the repo is detected, the config loads, `gh` runs, is logged in, and which GitHub user it acts as. Each failure prints how to fix it. Exits non-zero if any check fails. |
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

`prledger serve` listens on `127.0.0.1` only and answers only requests addressed to localhost. The page shows the last fetched data right away, then asks `gh` for the current status; use the **Refresh** button to ask again later. If a refresh fails (for example `gh` is logged out), the page keeps the data it has and shows the error.

Each pull request is one line. Open and draft ones show:

- **Where it stands**, on the right: CI checks (`✓ 20 checks passed`, `✗ 1 of 21 checks failed`, `… 3 of 9 checks running`), the review (`✓ approved`, `✗ changes requested`, `… review needed`) and merge trouble (`⊘ conflicts`, `↓ behind base`).
- **What it needs**, as a colored left edge and a word next to the number. **needs you** means conflicts, failing CI or requested changes. **approved** means approved with none of those. **waiting** means an open PR waiting on reviewers or CI. Drafts only ever get **needs you**.
- **Labels** in their GitHub colors, **reviewers** with a mark for where each stands (✓ approved, ✗ changes requested, … not reviewed yet), and **assignees** other than the author.

Merged and closed pull requests stay short: number, title, branch, labels and dates.

The page opens on **Active** (open and draft). The chips switch to one state, or to **Needs you**, **Approved** (every approved open PR, even one that also needs you) or **Waiting**. Search matches number, title, branch, label names and people.

GitHub only reports merge conflicts once it has checked the PR, so a PR it has not checked shows no merge note. Comment-only reviews are left out because they neither approve nor block. GitHub keeps a PR at "changes requested" until the reviewer approves, but once you have asked every such reviewer to look again, prledger shows it as **waiting** on them.

Reviews, CI and merge state come from a second, slower `gh` call that only asks about open PRs. Asking about every PR at once makes GitHub time out on busy repos. If that call fails twice (or takes over 40 seconds each time), the page and `list` still show every PR and say what is missing. `groups suggest` skips that call.

Fetched data is cached in `~/.cache/prledger` (or `$XDG_CACHE_HOME/prledger`), one file per repo, author and `gh` setup, so two accounts never see each other's data. Deleting it is safe.

## How grouping works

Each pull request lands in exactly one group, decided in this order:

1. **Stacks.** A PR is stacked when its base branch is the head branch of another of your PRs. Every PR linked that way, open, merged or closed, forms one stack, named after its bottom PR. Its PRs are in stack order, bottom first, each indented under the PR it sits on and saying where it sits ("2/8 · on #6730"). PRs whose base is a shared branch other than the default branch, for example `integration/phoenix`, form one stack per branch, named "on integration/phoenix", once at least two PRs sit on it.
2. A group in your config that lists the PR's number.
3. The first group in your config whose `branch` or `title` pattern matches.
4. Automatic grouping, if `auto_groups` is on. PRs are linked when they share a ticket key or a branch:
   - A ticket key such as `ABC-123` at the start of a branch path segment (`alice/abc-123-search`, `feature/ABC-123/ui`), or in upper case in the title (`fix: crash (ABC-123)`). Keys need at least two digits, and common words such as `SHA-256`, `UTF-16` or `fix-500` are skipped. If your tracker's prefixes are known, set `ticket_prefixes` and prledger stops guessing.
   - The same branch, or a split of it: `topic-split/part-1` joins `topic`.
   - Links chain: if A shares a key with B and B shares a branch with C, all three are one group. A group is named after its oldest PR.
5. Everything else goes to **Ungrouped**, including automatic groups of a single PR.

Groups are listed with the most recent work first; Ungrouped comes last.

A stack can only be seen while GitHub keeps the links: when a parent merges and GitHub retargets its child to the default branch, that child counts as a separate PR from then on.

## Releasing

Releases are cut locally with `make`; there are no CI workflows. From a clean `main`:

```sh
make check                          # gofmt, vet and race tests
make release-dry VERSION=v0.1.0     # build and verify everything, publish nothing
make release VERSION=v0.1.0         # tag, push the tag, publish the GitHub release
```

`make release` refuses uncommitted changes or a tag that already exists, runs `make check`, cross-builds archives for macOS, Linux and Windows (amd64 and arm64) with the version baked in, writes `checksums.txt`, checks that the built binary reports the version, then tags, pushes and runs `gh release create` with generated notes. Everything lands in `dist/`.

It only releases a commit that is already on `origin/main` (set `ALLOW_BRANCH=1` to release another branch). If the GitHub step fails after the tag was pushed, fix the cause and run `make publish VERSION=v0.1.0` from the tagged commit to finish; to start over instead, delete the tag with `git tag -d v0.1.0 && git push origin :refs/tags/v0.1.0`.

It publishes with whatever account `gh` is logged in as. To use another account, set its config folder for the run, for example `GH_CONFIG_DIR=~/.config/gh-personal make release VERSION=v0.1.0`, or pass the command as `GH="env GH_CONFIG_DIR=… gh"`.

## License

MIT
