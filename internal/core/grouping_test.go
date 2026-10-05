package core_test

import (
	"context"
	"math/rand/v2"
	"regexp"
	"strconv"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func pr(n int, branch, title string) core.PR {
	return core.PR{Number: n, Branch: branch, Title: title, CreatedAt: day(n)}
}

func snapshotOf(t *testing.T, g core.Grouping, prs ...core.PR) core.Snapshot {
	t.Helper()
	l := core.Ledger{Source: &fakeSource{prs: prs}, Now: clock, Grouping: g}
	snap, err := l.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// layout renders a snapshot as group name -> PR numbers, in order.
type layout []struct {
	name string
	prs  []int
}

func layoutOf(s core.Snapshot) layout {
	var out layout
	for _, g := range s.Groups {
		var nums []int
		for _, p := range g.PRs {
			nums = append(nums, p.Number)
		}
		out = append(out, struct {
			name string
			prs  []int
		}{g.Name, nums})
	}
	return out
}

func assertLayout(t *testing.T, got core.Snapshot, want layout) {
	t.Helper()
	g := layoutOf(got)
	if len(g) != len(want) {
		t.Fatalf("groups =\n%v\nwant\n%v", g, want)
	}
	for i := range want {
		if g[i].name != want[i].name || !equalInts(g[i].prs, want[i].prs) {
			t.Fatalf("groups =\n%v\nwant\n%v", g, want)
		}
	}
}

var auto = core.Grouping{Auto: true}

func TestExplicitNumbersBeatPatterns(t *testing.T) {
	g := core.Grouping{Rules: []core.GroupRule{
		{Name: "Search", Branch: regexp.MustCompile("search")},
		{Name: "Hand-picked", PRs: []int{2}},
	}}
	snap := snapshotOf(t, g, pr(1, "feat/search-ui", "a"), pr(2, "feat/search-api", "b"))
	assertLayout(t, snap, layout{{"Hand-picked", []int{2}}, {"Search", []int{1}}})
}

func TestFirstMatchingPatternWins(t *testing.T) {
	g := core.Grouping{Rules: []core.GroupRule{
		{Name: "By title", Title: regexp.MustCompile("(?i)login")},
		{Name: "By branch", Branch: regexp.MustCompile("^fix/")},
	}}
	snap := snapshotOf(t, g, pr(1, "fix/login", "Fix login"), pr(2, "fix/typo", "Fix typo"))
	assertLayout(t, snap, layout{{"By branch", []int{2}}, {"By title", []int{1}}})
}

func TestPatternsBeatAutomaticGrouping(t *testing.T) {
	g := core.Grouping{Auto: true, Rules: []core.GroupRule{{Name: "Docs", Title: regexp.MustCompile("^docs")}}}
	snap := snapshotOf(t, g,
		pr(1, "alice/abc-12-api", "feat: api"),
		pr(2, "alice/abc-12-docs", "docs: api"),
		pr(3, "alice/abc-12-ui", "feat: ui"),
	)
	assertLayout(t, snap, layout{{"ABC-12 · feat: api", []int{3, 1}}, {"Docs", []int{2}}})
}

func TestTicketKeysGroupAcrossBranchAndTitle(t *testing.T) {
	snap := snapshotOf(t, auto,
		pr(1, "alice/abc-12-api", "feat: api"),
		pr(2, "feature/ABC-12/ui", "feat: ui"),
		pr(3, "hotfix-ui", "fix: ui crash (ABC-12)"),
		pr(4, "alice/abc-12b-more", "feat: more"), // letter suffix, same ticket
	)
	assertLayout(t, snap, layout{{"ABC-12 · feat: api", []int{4, 3, 2, 1}}})
}

// "utf-8" and similar are not ticket keys: a key needs 2+ digits, counts only
// at the start of a branch path segment, and must be upper-case in a title.
func TestWordsWithNumbersAreNotTicketKeys(t *testing.T) {
	snap := snapshotOf(t, auto,
		pr(1, "quick-fix/ensure-utf-8-charset", "fix: ensure UTF-8 charset"),
		pr(2, "fix/utf-8-bom", "fix: drop the UTF-8 BOM"),
		pr(3, "area-access-01-shared", "feat: shared guard"),
		pr(4, "area-access-02-admin", "feat: admin guard"),
	)
	assertLayout(t, snap, layout{{core.UngroupedName, []int{4, 3, 2, 1}}})
}

func TestSplitBranchesJoinTheirBase(t *testing.T) {
	snap := snapshotOf(t, auto,
		pr(1, "alice/auto-compact", "feat: auto-compact"),
		pr(2, "alice/auto-compact-split/flag", "feat: flag"),
		pr(3, "alice/auto-compact-split/ui", "feat: ui"),
	)
	assertLayout(t, snap, layout{{"feat: auto-compact", []int{3, 2, 1}}})
}

func TestReusedBranchGroupsTogether(t *testing.T) {
	snap := snapshotOf(t, auto, pr(1, "alice/cache", "first try"), pr(2, "alice/cache", "second try"))
	assertLayout(t, snap, layout{{"first try", []int{2, 1}}})
}

// A PR that shares a key with one PR and a branch family with another links
// all three.
func TestSharedKeysChainIntoOneGroup(t *testing.T) {
	snap := snapshotOf(t, auto,
		pr(1, "alice/abc-55-x", "one"),
		pr(2, "alice/abc-55-x-split/y", "two"),
		pr(3, "alice/other-split/z", "three (ABC-55)"),
		pr(4, "alice/other", "four"),
	)
	assertLayout(t, snap, layout{{"ABC-55 · one", []int{4, 3, 2, 1}}})
}

func TestSinglePRAutomaticGroupsStayUngrouped(t *testing.T) {
	snap := snapshotOf(t, auto, pr(1, "alice/abc-11-x", "one"), pr(2, "alice/xyz-22-y", "two"))
	assertLayout(t, snap, layout{{core.UngroupedName, []int{2, 1}}})
}

func TestAutomaticGroupingCanBeTurnedOff(t *testing.T) {
	snap := snapshotOf(t, core.Grouping{Auto: false}, pr(1, "alice/abc-11-x", "one"), pr(2, "alice/abc-11-y", "two"))
	assertLayout(t, snap, layout{{core.UngroupedName, []int{2, 1}}})
}

func TestGroupsSortNewestFirstAndUngroupedLast(t *testing.T) {
	g := core.Grouping{Auto: true, Rules: []core.GroupRule{{Name: "Old work", PRs: []int{1, 2}}}}
	snap := snapshotOf(t, g,
		pr(1, "a", "one"), pr(2, "b", "two"),
		pr(9, "lonely", "nine"),
		pr(5, "c/abc-33-x", "five"), pr(6, "c/abc-33-y", "six"),
	)
	assertLayout(t, snap, layout{
		{"ABC-33 · five", []int{6, 5}},
		{"Old work", []int{2, 1}},
		{core.UngroupedName, []int{9}},
	})
}

func TestRulesThatMatchNothingAreLeftOut(t *testing.T) {
	g := core.Grouping{Rules: []core.GroupRule{{Name: "Empty", PRs: []int{42}}}}
	snap := snapshotOf(t, g, pr(1, "a", "one"))
	assertLayout(t, snap, layout{{core.UngroupedName, []int{1}}})
}

// Whatever the PRs and rules, each PR lands in exactly one group.
func TestEveryPRLandsInExactlyOneGroup(t *testing.T) {
	words := []string{"abc-1", "abc-12", "ABC-12", "xyz-7", "utf-8", "fix", "feat", "alice", "cache", "-split/", "ui", "api"}
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 99))
		pick := func() string { return words[r.IntN(len(words))] }
		var prs []core.PR
		for n := 1; n <= 1+r.IntN(40); n++ {
			branch := pick() + "/" + pick() + "-" + pick()
			if r.IntN(4) == 0 && len(prs) > 0 {
				branch = prs[r.IntN(len(prs))].Branch + "-split/" + pick()
			}
			prs = append(prs, core.PR{Number: n, Branch: branch, Title: pick() + " " + pick(), CreatedAt: day(1 + r.IntN(28))})
		}
		g := core.Grouping{Auto: r.IntN(2) == 0}
		for i := range r.IntN(4) {
			rule := core.GroupRule{Name: "rule" + strconv.Itoa(i), PRs: []int{1 + r.IntN(40)}}
			if r.IntN(2) == 0 {
				rule.Branch = regexp.MustCompile(regexp.QuoteMeta(pick()))
			}
			g.Rules = append(g.Rules, rule)
		}

		snap := snapshotOf(t, g, prs...)
		seen := map[int]int{}
		for _, grp := range snap.Groups {
			if len(grp.PRs) == 0 {
				t.Fatalf("seed %d: empty group %q", seed, grp.Name)
			}
			for _, p := range grp.PRs {
				seen[p.Number]++
			}
		}
		for _, p := range prs {
			if seen[p.Number] != 1 {
				t.Fatalf("seed %d: PR #%d appears %d times", seed, p.Number, seen[p.Number])
			}
		}
	}
}
