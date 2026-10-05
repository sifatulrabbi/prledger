package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/sifatulrabbi/prledger/internal/cli"
)

// version is set at release time with -ldflags "-X main.version=vX.Y.Z".
var version string

func main() {
	info, _ := debug.ReadBuildInfo()
	root := cli.NewRoot(cli.Deps{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: cli.ResolveVersion(version, info),
	})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "prledger:", err)
		os.Exit(1)
	}
}
