package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func newListCmd(d Deps, g *globalFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your pull requests in this repo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snap, err := snapshot(cmd, d, g)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), snap)
			}
			return writeTable(cmd.OutOrStdout(), snap)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the snapshot as JSON")
	return cmd
}

func writeJSON(w io.Writer, snap core.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

const maxTitle = 72

func writeTable(w io.Writer, snap core.Snapshot) error {
	total := 0
	for _, g := range snap.Groups {
		total += len(g.PRs)
	}
	fmt.Fprintf(w, "%s · %d pull requests by %s · fetched %s\n", snap.Repo, total, snap.Author, snap.FetchedAt.Format("2006-01-02 15:04 MST"))
	for _, g := range snap.Groups {
		fmt.Fprintf(w, "\n%s (%d)\n", g.Name, len(g.PRs))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, pr := range g.PRs {
			fmt.Fprintf(tw, "  #%d\t%s\t%s\t%s\n", pr.Number, pr.Status, truncate(pr.Title, maxTitle), pr.Branch)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
