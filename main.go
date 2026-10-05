package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/sifatulrabbi/prledger/internal/cli"
	"github.com/sifatulrabbi/prledger/internal/core"
	"github.com/sifatulrabbi/prledger/internal/gh"
	"github.com/sifatulrabbi/prledger/internal/gitremote"
)

// version is set at release time with -ldflags "-X main.version=vX.Y.Z".
var version string

func main() {
	info, _ := debug.ReadBuildInfo()
	root := cli.NewRoot(cli.Deps{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: cli.ResolveVersion(version, info),
		Now:     time.Now,
		Runner:  gh.ExecRunner{},
		Getenv:  os.Getenv,
		DetectRepo: func(ctx context.Context) (core.Repo, error) {
			dir, err := os.Getwd()
			if err != nil {
				return core.Repo{}, err
			}
			return gitremote.Origin(ctx, dir)
		},
	})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "prledger:", err)
		os.Exit(1)
	}
}
