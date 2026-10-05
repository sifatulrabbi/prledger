# prledger

See every pull request you've opened in a repo, grouped by feature, with its current status.

prledger runs inside any git repo and asks the [GitHub CLI](https://cli.github.com) (`gh`) for your PRs. It never handles GitHub tokens itself: whatever account `gh` is logged in as is the account prledger uses.

> Work in progress. Commands below are being added one by one.

## Install

```sh
go install github.com/sifatulrabbi/prledger@latest
```

You need `gh` installed and logged in (`gh auth login`).

## Usage

```sh
prledger version
```

## License

MIT
