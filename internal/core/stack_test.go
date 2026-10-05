package core_test

import (
	"math/rand/v2"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// on returns a PR whose head is branch and whose base is base.
func on(n int, branch, base string) core.PR {
	p := pr(n, branch, "pr "+branch)
	p.Base = base
	return p
}

func arranged(t *testing.T, g core.Grouping, defaultBranch string, prs ...core.PR) core.Snapshot {
	t.Helper()
	return core.Ledger{Grouping: g}.Regroup(core.Snapshot{
		DefaultBranch: defaultBranch,
		Groups:        []core.Group{{Name: "all", PRs: prs}},
	})
}

// stackOf renders a stack group as number/parent/depth triples, in order.
func stackOf(t *testing.T, g core.Group) [][3]int {
	t.Helper()
	if g.Stack == nil {
		t.Fatalf("group %q is not a stack", g.Name)
	}
	if len(g.Stack.Entries) != len(g.PRs) {
		t.Fatalf("group %q: %d entries for %d PRs", g.Name, len(g.Stack.Entries), len(g.PRs))
	}
	var out [][3]int
	for i, e := range g.Stack.Entries {
		if e.Number != g.PRs[i].Number {
			t.Fatalf("group %q: entry %d is #%d but PR is #%d", g.Name, i, e.Number, g.PRs[i].Number)
		}
		out = append(out, [3]int{e.Number, e.On, e.Depth})
	}
	return out
}

func TestAChainOfPRsIsOneStackInOrder(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "main",
		on(3, "c", "b"),
		on(1, "a", "main"),
		on(2, "b", "a"),
	)
	if len(snap.Groups) != 1 || snap.Groups[0].Name != "pr a" {
		t.Fatalf("groups = %v, want one stack named after its bottom PR", layoutOf(snap))
	}
	if got, want := stackOf(t, snap.Groups[0]), [][3]int{{1, 0, 0}, {2, 1, 1}, {3, 2, 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stack = %v, want %v", got, want)
	}
	if snap.Groups[0].Stack.Base != "" {
		t.Fatalf("base = %q, want empty for a stack on the default branch", snap.Groups[0].Stack.Base)
	}
}

func TestATreeIsWalkedParentFirstChildrenOldestFirst(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "main",
		on(1, "a", "main"),
		on(2, "b", "a"),
		on(3, "c", "a"),
		on(4, "d", "c"),
	)
	if got, want := stackOf(t, snap.Groups[0]), [][3]int{{1, 0, 0}, {2, 1, 1}, {3, 1, 1}, {4, 3, 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stack = %v, want %v", got, want)
	}
}

func TestMergedAndClosedPRsStayInTheirStack(t *testing.T) {
	bottom, mid, top := on(1, "a", "main"), on(2, "b", "a"), on(3, "c", "b")
	bottom.Status, mid.Status, top.Status = core.StatusMerged, core.StatusClosed, core.StatusOpen
	snap := arranged(t, core.Grouping{}, "main", bottom, mid, top)
	if len(snap.Groups) != 1 || len(snap.Groups[0].PRs) != 3 {
		t.Fatalf("groups = %v, want all three in one stack", layoutOf(snap))
	}
}

func TestPRsOnASharedBranchAreAStackOnThatBranch(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "main",
		on(5, "x", "integration/phoenix"),
		on(6, "y", "integration/phoenix"),
		on(7, "z", "y"),
		on(8, "loose", "main"),
	)
	stack := snap.Groups[0]
	if stack.Name != "on integration/phoenix" || stack.Stack.Base != "integration/phoenix" {
		t.Fatalf("group = %q base %q", stack.Name, stack.Stack.Base)
	}
	if got, want := stackOf(t, stack), [][3]int{{5, 0, 0}, {6, 0, 0}, {7, 6, 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stack = %v, want %v", got, want)
	}
	assertLayout(t, snap, layout{{"on integration/phoenix", []int{5, 6, 7}}, {core.UngroupedName, []int{8}}})
}

func TestALonePROnTheDefaultBranchIsLoose(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "main", on(1, "a", "main"))
	assertLayout(t, snap, layout{{core.UngroupedName, []int{1}}})
	if snap.Groups[0].Stack != nil {
		t.Fatal("Ungrouped must not be a stack")
	}
}

// With the default branch unknown (a cache from before stacks), only
// PR-on-PR stacking can be told apart.
func TestWithoutADefaultBranchOnlyPROnPRStacksForm(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "",
		on(1, "a", "main"),
		on(2, "b", "a"),
		on(3, "c", "develop"),
	)
	assertLayout(t, snap, layout{{"pr a", []int{1, 2}}, {core.UngroupedName, []int{3}}})
}

// Regression: cached snapshots from before stacks have no base, and an empty
// base matched every PR with an empty head, stacking unrelated PRs.
func TestPRsWithoutABaseAreNeverStacked(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "main", on(1, "", ""), on(2, "", ""), on(3, "a", ""))
	assertLayout(t, snap, layout{{core.UngroupedName, []int{3, 2, 1}}})
}

func TestStacksComeBeforeConfigRules(t *testing.T) {
	g := core.Grouping{Rules: []core.GroupRule{{Name: "Picked", PRs: []int{2, 9}}, {Name: "Pattern", Branch: regexp.MustCompile("^c$")}}}
	snap := arranged(t, g, "main",
		on(1, "a", "main"),
		on(2, "b", "a"),
		on(3, "c", "b"),
		on(9, "lone", "main"),
	)
	assertLayout(t, snap, layout{{"Picked", []int{9}}, {"pr a", []int{1, 2, 3}}})
}

// Branches can be reused or retargeted into a loop; the stack still holds
// every PR once and the walk ends.
func TestACycleOfBasesStillEnds(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "main", on(1, "x", "y"), on(2, "y", "x"))
	if len(snap.Groups) != 1 || len(snap.Groups[0].PRs) != 2 {
		t.Fatalf("groups = %v, want one stack of both", layoutOf(snap))
	}
	stackOf(t, snap.Groups[0])
}

// A reused head branch: a child sits on the PR with that head that was
// created most recently before it.
func TestAChildSitsOnTheLatestEarlierPRWithItsBase(t *testing.T) {
	snap := arranged(t, core.Grouping{}, "main",
		on(1, "a", "main"),
		on(2, "a", "main"), // the branch was reused for a second PR
		on(3, "b", "a"),
	)
	got := stackOf(t, snap.Groups[0])
	if want := [][3]int{{1, 0, 0}, {2, 0, 0}, {3, 2, 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stack = %v, want %v", got, want)
	}
}

func TestSuggestLeavesStackedPRsOut(t *testing.T) {
	groups, alone := core.Grouping{}.Suggest([]core.PR{
		on(1, "a/abc-11-x", "main"),
		on(2, "a/abc-11-y", "a/abc-11-x"),
		on(3, "a/abc-11-z", "main"),
		on(4, "a/abc-11-w", "main"),
	}, "main")
	if len(groups) != 1 || !equalInts(numbers(groups[0].PRs), []int{4, 3}) || len(alone) != 0 {
		t.Fatalf("groups %v alone %v, want only #3 and #4 suggested", groups, alone)
	}
}

// Whatever the order PRs arrive in, stacks come out the same.
func TestStacksDoNotDependOnPROrder(t *testing.T) {
	branches := []string{"main", "integration/x", "a", "b", "c", "d", "e", "f"}
	for seed := range uint64(200) {
		r := rand.New(rand.NewPCG(seed, 3))
		var prs []core.PR
		for n := 1; n <= 2+r.IntN(12); n++ {
			prs = append(prs, on(n, branches[2+r.IntN(len(branches)-2)], branches[r.IntN(len(branches))]))
		}
		want := layoutOf(arranged(t, core.Grouping{Auto: true}, "main", prs...))
		shuffled := slices.Clone(prs)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := arranged(t, core.Grouping{Auto: true}, "main", shuffled...)
		if !reflect.DeepEqual(layoutOf(got), want) {
			t.Fatalf("seed %d: order changed the result:\n%v\nvs\n%v", seed, layoutOf(got), want)
		}
		seen := map[int]int{}
		for _, grp := range got.Groups {
			for _, p := range grp.PRs {
				seen[p.Number]++
			}
		}
		for _, p := range prs {
			if seen[p.Number] != 1 {
				t.Fatalf("seed %d: #%d appears %d times", seed, p.Number, seen[p.Number])
			}
		}
	}
}

func numbers(prs []core.PR) []int {
	var out []int
	for _, p := range prs {
		out = append(out, p.Number)
	}
	return out
}
