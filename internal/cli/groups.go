package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func newGroupsCmd(d Deps, g *globalFlags) *cobra.Command {
	groups := &cobra.Command{
		Use:   "groups",
		Short: "Help write the groups in your config",
	}
	var cached bool
	suggest := &cobra.Command{
		Use:   "suggest",
		Short: "Print a groups: block for this repo, ready to paste into the config",
		Long:  "Groups the pull requests that no configured group claims, using the automatic rules, and prints them as config YAML. Paste it under the repo's section and rename the groups.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := resolveTarget(cmd, d, g)
			if err != nil {
				return err
			}
			var snap core.Snapshot
			if cached {
				snap, err = loadCached(t, d)
			} else {
				snap, err = fetchFresh(cmd, t, d)
			}
			if err != nil {
				return err
			}
			var prs []core.PR
			for _, grp := range snap.Groups {
				prs = append(prs, grp.PRs...)
			}
			suggested, alone := t.ledger(d).Grouping.Suggest(prs)
			return writeSuggestion(cmd.OutOrStdout(), t.repo, suggested, alone)
		},
	}
	suggest.Flags().BoolVar(&cached, "cached", false, "use the last fetched snapshot without calling gh")
	groups.AddCommand(suggest)
	return groups
}

type suggestedGroup struct {
	Name string `yaml:"name"`
	PRs  []int  `yaml:"prs,flow"`
}

func writeSuggestion(w io.Writer, repo core.Repo, groups []core.Group, alone []core.PR) error {
	fmt.Fprintf(w, "# Suggested groups for %s. Paste under repos.%s in your config\n# (prledger config path) and rename them.\n", repo, repo)
	out := struct {
		Groups []suggestedGroup `yaml:"groups"`
	}{Groups: []suggestedGroup{}}
	for _, g := range groups {
		sg := suggestedGroup{Name: g.Name}
		for _, p := range g.PRs {
			sg.PRs = append(sg.PRs, p.Number)
		}
		out.Groups = append(out.Groups, sg)
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	if len(alone) > 0 {
		fmt.Fprintf(w, "# Pull requests prledger could not link to others (add them to a group by hand):\n")
		for _, p := range alone {
			fmt.Fprintf(w, "#   %d  %s\n", p.Number, p.Title)
		}
	}
	return nil
}
