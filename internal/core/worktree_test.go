package core_test

import (
	"math/rand/v2"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func hour(h int) time.Time { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC) }

func worktree(name string, current string, visits map[string]int) core.Worktree {
	w := core.Worktree{Name: name, Path: "/wt/" + name, Current: current, Branches: map[string]time.Time{}}
	for b, h := range visits {
		w.Branches[b] = hour(h)
	}
	return w
}

func TestPRsGroupByTheWorktreeThatCheckedThemOut(t *testing.T) {
	g := core.Grouping{Worktrees: []core.Worktree{
		worktree("rename", "alice/abc-63", map[string]int{"alice/abc-61": 1, "alice/abc-62": 2, "alice/abc-63": 3}),
	}}
	snap := snapshotOf(t, g,
		pr(1, "alice/abc-61", "vocabulary"),
		pr(2, "alice/abc-62", "usage column"),
		pr(3, "alice/abc-63", "drop old column"),
		pr(4, "alice/elsewhere", "other work"),
	)
	assertLayout(t, snap, layout{{"rename", []int{3, 2, 1}}, {core.UngroupedName, []int{4}}})
	if snap.Groups[0].Worktree != "/wt/rename" {
		t.Fatalf("worktree = %q, want the worktree path on the group", snap.Groups[0].Worktree)
	}
	if snap.Groups[1].Worktree != "" {
		t.Fatalf("Ungrouped worktree = %q, want empty", snap.Groups[1].Worktree)
	}
}

// A worktree is a deliberate stream of work, so one PR is enough for a group
// (unlike automatic groups).
func TestAWorktreeWithOnePRIsAGroup(t *testing.T) {
	g := core.Grouping{Worktrees: []core.Worktree{worktree("lovable", "alice/lovable-button", nil)}}
	snap := snapshotOf(t, g, pr(1, "alice/lovable-button", "Open in Lovable"))
	assertLayout(t, snap, layout{{"lovable", []int{1}}})
}

func TestWorktreesSitBetweenConfigRulesAndAutomaticGrouping(t *testing.T) {
	g := core.Grouping{
		Auto: true,
		Rules: []core.GroupRule{
			{Name: "Picked", PRs: []int{1}},
			{Name: "Docs", Title: regexp.MustCompile("^docs")},
		},
		Worktrees: []core.Worktree{worktree("feature", "", map[string]int{"a/abc-11-x": 1, "a/abc-11-y": 1, "a/abc-11-z": 1, "a/abc-11-docs": 1})},
	}
	snap := snapshotOf(t, g,
		pr(1, "a/abc-11-x", "picked by number"),
		pr(2, "a/abc-11-docs", "docs: by pattern"),
		pr(3, "a/abc-11-y", "by worktree"),
		pr(4, "a/abc-11-z", "by worktree too"),
		pr(5, "b/abc-11-w", "linked to the worktree's PRs by ABC-11, so it joins them"),
		pr(6, "c/xyz-22-v", "automatic, linked to #7"),
		pr(7, "c/xyz-22-u", "automatic, linked to #6"),
	)
	assertLayout(t, snap, layout{
		{"XYZ-22 · automatic, linked to #7", []int{7, 6}},
		{"feature", []int{5, 4, 3}},
		{"Docs", []int{2}},
		{"Picked", []int{1}},
	})
}

func TestABranchInSeveralWorktreesGoesWhereItIsCheckedOut(t *testing.T) {
	g := core.Grouping{Worktrees: []core.Worktree{
		worktree("old", "", map[string]int{"shared": 9}),
		worktree("current", "shared", map[string]int{"shared": 1}),
	}}
	snap := snapshotOf(t, g, pr(1, "shared", "x"))
	assertLayout(t, snap, layout{{"current", []int{1}}})
}

func TestOtherwiseTheMostRecentCheckoutWins(t *testing.T) {
	g := core.Grouping{Worktrees: []core.Worktree{
		worktree("earlier", "", map[string]int{"shared": 1}),
		worktree("later", "", map[string]int{"shared": 5}),
	}}
	snap := snapshotOf(t, g, pr(1, "shared", "x"))
	assertLayout(t, snap, layout{{"later", []int{1}}})
}

// Stacked branches are often created without being checked out, so the
// worktree's reflog never sees them. With automatic grouping on, PRs that
// share a ticket key or branch family with a worktree's PRs join it.
func TestLinkedPRsJoinTheWorktree(t *testing.T) {
	g := core.Grouping{Auto: true, Worktrees: []core.Worktree{worktree("translation", "a/abc-97-workspace", nil)}}
	snap := snapshotOf(t, g,
		pr(1, "a/abc-97-package", "package"),       // shares ABC-97, never checked out
		pr(2, "a/abc-97-package-split/api", "api"), // links to #1 by branch family: a chain
		pr(3, "a/abc-97-workspace", "workspace"),   // checked out in the worktree
		pr(4, "a/unrelated", "unrelated"),
	)
	assertLayout(t, snap, layout{{"translation", []int{3, 2, 1}}, {core.UngroupedName, []int{4}}})
}

func TestAPRLinkedToTwoWorktreesIsLeftToAutomaticGrouping(t *testing.T) {
	g := core.Grouping{Auto: true, Worktrees: []core.Worktree{
		worktree("first", "a/abc-11-x", nil),
		worktree("second", "a/abc-11-y", nil),
	}}
	snap := snapshotOf(t, g, pr(1, "a/abc-11-x", "x"), pr(2, "a/abc-11-y", "y"), pr(3, "a/abc-11-z", "z"))
	assertLayout(t, snap, layout{{"second", []int{2}}, {"first", []int{1}}, {core.UngroupedName, []int{3}}})
}

func TestWorktreesDoNotPullInLinkedPRsWhenAutomaticGroupingIsOff(t *testing.T) {
	g := core.Grouping{Auto: false, Worktrees: []core.Worktree{worktree("translation", "a/abc-97-workspace", nil)}}
	snap := snapshotOf(t, g, pr(1, "a/abc-97-package", "package"), pr(2, "a/abc-97-workspace", "workspace"))
	assertLayout(t, snap, layout{{"translation", []int{2}}, {core.UngroupedName, []int{1}}})
}

// Regression: PRs joined a worktree one at a time, so the result depended on
// the order PRs arrived in, and `list` and `list --cached` (which flattens
// groups) disagreed on the same data.
func TestWorktreeLinkingDoesNotDependOnPROrder(t *testing.T) {
	g := core.Grouping{Auto: true, Worktrees: []core.Worktree{
		worktree("w0", "a/abc-11-x", nil),
		worktree("w1", "b/xyz-33-y", nil),
	}}
	prs := []core.PR{
		pr(1, "a/abc-11-x", "w0's"),
		pr(2, "b/xyz-33-y", "w1's"),
		pr(3, "d/q-feature", "q (QQQ-22) (XYZ-33)"),
		pr(4, "c/abc-11-p", "p (QQQ-22)"),
		pr(5, "d/q-feature-split/a", "slice"),
	}
	want := layoutOf(snapshotOf(t, g, prs...))
	for seed := range uint64(50) {
		shuffled := slices.Clone(prs)
		rand.New(rand.NewPCG(seed, 7)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := layoutOf(snapshotOf(t, g, shuffled...)); !reflect.DeepEqual(got, want) {
			t.Fatalf("order %v gives\n%v\nwant\n%v", numbers(shuffled), got, want)
		}
	}
}

// #3, #4 and #5 are linked to each other and, through them, to both w0 and
// w1: an ambiguous set stays with automatic grouping, whole.
func TestALinkedSetTouchingTwoWorktreesStaysTogether(t *testing.T) {
	g := core.Grouping{Auto: true, Worktrees: []core.Worktree{
		worktree("w0", "a/abc-11-x", nil),
		worktree("w1", "b/xyz-33-y", nil),
	}}
	snap := snapshotOf(t, g,
		pr(1, "a/abc-11-x", "w0's"),
		pr(2, "b/xyz-33-y", "w1's"),
		pr(3, "d/q-feature", "q (QQQ-22) (XYZ-33)"),
		pr(4, "c/abc-11-p", "p (QQQ-22)"),
		pr(5, "d/q-feature-split/a", "slice"),
	)
	assertLayout(t, snap, layout{
		{"ABC-11 +2 · q (QQQ-22) (XYZ-33)", []int{5, 4, 3}},
		{"w1", []int{2}},
		{"w0", []int{1}},
	})
}

// Regression: the SEQ-28 worktree's branch had no PR of its own, so it
// pulled in nothing; its branch names are links too.
func TestAWorktreeBranchWithoutAPRStillPullsInLinkedPRs(t *testing.T) {
	g := core.Grouping{Auto: true, Worktrees: []core.Worktree{worktree("persona", "a/abc-28-authz", nil)}}
	snap := snapshotOf(t, g, pr(1, "a/abc-28-server-auth", "server auth"))
	assertLayout(t, snap, layout{{"persona", []int{1}}})
}

func TestEqualCheckoutTimesGoToTheEarlierWorktree(t *testing.T) {
	g := core.Grouping{Worktrees: []core.Worktree{
		worktree("first", "", map[string]int{"shared": 3}),
		worktree("second", "", map[string]int{"shared": 3}),
	}}
	assertLayout(t, snapshotOf(t, g, pr(1, "shared", "x")), layout{{"first", []int{1}}})
}

// Group names are what people read; two groups never share one, and
// "Ungrouped" always means ungrouped.
func TestGroupNamesStayUnique(t *testing.T) {
	g := core.Grouping{
		Rules: []core.GroupRule{{Name: "feat", PRs: []int{1}}},
		Worktrees: []core.Worktree{
			worktree("feat", "b", nil),
			{Name: "feat", Path: "/other/feat", Current: "c"},
			worktree(core.UngroupedName, "d", nil),
		},
	}
	snap := snapshotOf(t, g, pr(1, "a", "1"), pr(2, "b", "2"), pr(3, "c", "3"), pr(4, "d", "4"), pr(5, "e", "5"))
	seen := map[string]bool{}
	for _, grp := range snap.Groups {
		if seen[grp.Name] {
			t.Fatalf("duplicate group name %q in %v", grp.Name, layoutOf(snap))
		}
		seen[grp.Name] = true
		if grp.Name == core.UngroupedName && !equalInts(numbers(grp.PRs), []int{5}) {
			t.Fatalf("Ungrouped holds %v, want only #5", numbers(grp.PRs))
		}
	}
}

// Regression: suggest pulled linked PRs into worktrees even with automatic
// grouping off, so a PR shown in Ungrouped was neither suggested nor listed.
func TestSuggestMatchesListWhenAutomaticGroupingIsOff(t *testing.T) {
	g := core.Grouping{Auto: false, Worktrees: []core.Worktree{worktree("mine", "a/abc-11-x", nil)}}
	_, alone := g.Suggest([]core.PR{pr(1, "a/abc-11-x", "in the worktree"), pr(2, "a/abc-11-y", "linked, but auto is off")})
	if len(alone) != 1 || alone[0].Number != 2 {
		t.Fatalf("alone = %+v, want #2 listed as not linked", alone)
	}
}

func numbers(prs []core.PR) []int {
	var out []int
	for _, p := range prs {
		out = append(out, p.Number)
	}
	return out
}

func TestSuggestLeavesWorktreePRsOut(t *testing.T) {
	g := core.Grouping{Auto: true, Worktrees: []core.Worktree{worktree("mine", "a/abc-11-x", nil)}}
	groups, alone := g.Suggest([]core.PR{
		pr(1, "a/abc-11-x", "in the worktree"),
		pr(2, "a/abc-11-y", "linked to the worktree"),
		pr(3, "b/xyz-22-a", "x"),
		pr(4, "b/xyz-22-b", "y"),
	})
	if len(groups) != 1 || groups[0].Name != "XYZ-22 · x" || len(alone) != 0 {
		t.Fatalf("groups %+v alone %+v, want only XYZ-22 suggested", groups, alone)
	}
}
