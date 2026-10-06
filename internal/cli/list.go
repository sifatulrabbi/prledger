package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func newListCmd(d Deps, g *globalFlags) *cobra.Command {
	var asJSON, cached bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your pull requests in this repo",
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
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), snap)
			}
			return writeTable(cmd.OutOrStdout(), snap)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the snapshot as JSON")
	cmd.Flags().BoolVar(&cached, "cached", false, "show the last fetched snapshot without calling gh")
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
		kind := ""
		if g.Stack != nil {
			kind = " · stack"
		}
		fmt.Fprintf(w, "\n%s (%d)%s\n", g.Name, len(g.PRs), kind)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for i, pr := range g.PRs {
			title := truncate(pr.Title, maxTitle)
			if g.Stack != nil && g.Stack.Entries[i].Depth > 0 {
				// Indent stacked PRs under the PR they sit on.
				title = strings.Repeat("  ", g.Stack.Entries[i].Depth-1) + "└ " + title
			}
			fmt.Fprintf(tw, "  #%d\t%s\t%s\t%s\t%s\n", pr.Number, pr.Status, title, pr.Branch, signals(pr))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}

var (
	checkWords  = map[core.CheckState]string{core.CheckPass: "ci ✓", core.CheckFail: "ci ✗", core.CheckPending: "ci …"}
	reviewWords = map[core.Review]string{core.ReviewApproved: "approved", core.ReviewChanges: "changes requested", core.ReviewRequired: "review needed"}
	mergeWords  = map[core.Merge]string{core.MergeConflicting: "conflicts", core.MergeBehind: "behind base"}
)

// signals sums up an open PR's CI, review and merge state in a few words.
func signals(pr core.PR) string {
	var words []string
	if pr.Checks != nil {
		words = append(words, checkWords[pr.Checks.State])
	}
	for _, w := range []string{reviewWords[pr.Review], mergeWords[pr.Merge]} {
		if w != "" {
			words = append(words, w)
		}
	}
	return strings.Join(words, " · ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
