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
	"github.com/sifatulrabbi/prledger/internal/gh"
	"github.com/sifatulrabbi/prledger/internal/gitremote"
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
		DetectRepo:  inWorkingDir(gitremote.Origin),
	})
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "prledger:", err)
		os.Exit(1)
	}
}

// inWorkingDir runs a git reader against the current directory.
func inWorkingDir[T any](read func(context.Context, string) (T, error)) func(context.Context) (T, error) {
	return func(ctx context.Context) (T, error) {
		dir, err := os.Getwd()
		if err != nil {
			var zero T
			return zero, err
		}
		return read(ctx, dir)
	}
}
