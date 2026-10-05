package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/sifatulrabbi/prledger/internal/browser"
	"github.com/sifatulrabbi/prledger/internal/cli"
	"github.com/sifatulrabbi/prledger/internal/core"
	"github.com/sifatulrabbi/prledger/internal/gh"
	"github.com/sifatulrabbi/prledger/internal/gitremote"
	"github.com/sifatulrabbi/prledger/internal/gitworktree"
)

// version is set at release time with -ldflags "-X main.version=vX.Y.Z".
var version string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	info, _ := debug.ReadBuildInfo()
	root := cli.NewRoot(cli.Deps{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Version:     cli.ResolveVersion(version, info),
		Now:         time.Now,
		Runner:      gh.ExecRunner{},
		Getenv:      os.Getenv,
		OpenBrowser: browser.Open,
		DetectRepo: func(ctx context.Context) (core.Repo, error) {
			dir, err := os.Getwd()
			if err != nil {
				return core.Repo{}, err
			}
			return gitremote.Origin(ctx, dir)
		},
		Worktrees: func(ctx context.Context) ([]core.Worktree, error) {
			dir, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			return gitworktree.List(ctx, dir)
		},
	})
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "prledger:", err)
		os.Exit(1)
	}
}
