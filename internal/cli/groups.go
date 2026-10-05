package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode"

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
			snap, err := snapshotFor(cmd, t, d, cached)
			if err != nil {
				return err
			}
			var prs []core.PR
			for _, grp := range snap.Groups {
				prs = append(prs, grp.PRs...)
			}
			suggested, alone := t.ledger(d).Grouping.Suggest(prs)
			var configured []string
			for _, rule := range t.settings.Groups {
				configured = append(configured, rule.Name)
			}
			return writeSuggestion(cmd.OutOrStdout(), t.repo, suggested, alone, configured)
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

// writeSuggestion prints the groups as config YAML. With groups already
// configured it prints only list items to append under the existing groups:
// key (a second groups: key would not load), and keeps every name unique.
func writeSuggestion(w io.Writer, repo core.Repo, groups []core.Group, alone []core.PR, configured []string) error {
	taken := map[string]bool{}
	for _, n := range configured {
		taken[n] = true
	}
	items := []suggestedGroup{}
	for _, g := range groups {
		sg := suggestedGroup{Name: uniqueName(g.Name, taken)}
		for _, p := range g.PRs {
			sg.PRs = append(sg.PRs, p.Number)
		}
		items = append(items, sg)
	}

	var doc any = struct {
		Groups []suggestedGroup `yaml:"groups"`
	}{items}
	switch {
	case len(items) == 0:
		fmt.Fprintf(w, "# No new groups to suggest for %s.\n", repo)
		doc = nil
	case len(configured) > 0:
		fmt.Fprintf(w, "# Suggested groups for %s. Append these under the existing groups: list in\n# repos.%s in your config (prledger config path) and rename them.\n", repo, repo)
		doc = items
	default:
		fmt.Fprintf(w, "# Suggested groups for %s. Paste under repos.%s in your config\n# (prledger config path) and rename them.\n", repo, repo)
	}
	if doc != nil {
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(doc); err != nil {
			return err
		}
		if err := enc.Close(); err != nil {
			return err
		}
	}
	if len(alone) > 0 {
		fmt.Fprintf(w, "# Pull requests prledger could not link to others (add them to a group by hand):\n")
		for _, p := range alone {
			fmt.Fprintf(w, "#   %d  %s\n", p.Number, oneLine(p.Title))
		}
	}
	return nil
}

// uniqueName returns name, or name with " (2)", " (3)"… if it is taken, and
// marks the result taken.
func uniqueName(name string, taken map[string]bool) string {
	candidate := name
	for n := 2; taken[candidate]; n++ {
		candidate = fmt.Sprintf("%s (%d)", name, n)
	}
	taken[candidate] = true
	return candidate
}

// oneLine replaces control characters and Unicode line breaks with spaces, so
// text printed in a YAML comment cannot start a new line.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, s)
}
