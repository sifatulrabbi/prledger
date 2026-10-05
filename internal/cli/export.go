package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/prledger/internal/web"
)

func newExportCmd(d Deps, g *globalFlags) *cobra.Command {
	export := &cobra.Command{
		Use:   "export",
		Short: "Write your pull requests to a file",
	}
	var out string
	var cached bool
	html := &cobra.Command{
		Use:   "html",
		Short: "Write a standalone HTML page that needs no server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := resolveTarget(cmd, d, g)
			if err != nil {
				return err
			}
			snap, err := snapshotFor(cmd, t, d, cached)
			if err != nil {
				return err
			}
			// An export is meant to be shared; local worktree paths stay home.
			for i := range snap.Groups {
				snap.Groups[i].Worktree = ""
			}
			page, err := web.Export(snap)
			if err != nil {
				return err
			}
			path := out
			if path == "" {
				path = fmt.Sprintf("prledger-%s-%s.html", t.repo.Owner, t.repo.Name)
			}
			if err := os.WriteFile(path, page, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	}
	html.Flags().StringVarP(&out, "output", "o", "", "file to write (default: prledger-<owner>-<repo>.html here)")
	html.Flags().BoolVar(&cached, "cached", false, "use the last fetched snapshot without calling gh")
	export.AddCommand(html)
	return export
}
