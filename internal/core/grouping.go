package core

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// GroupRule is a group the user defined. A PR joins it when its number is
// listed, or when its branch or title matches one of the patterns.
type GroupRule struct {
	Name   string
	PRs    []int
	Branch *regexp.Regexp // optional
	Title  *regexp.Regexp // optional
}

// Grouping decides which group each PR lands in. Precedence: a rule that
// lists the PR's number, then the first rule whose pattern matches, then (if
// Auto) automatic grouping, then Ungrouped.
type Grouping struct {
	Rules []GroupRule
	Auto  bool
}

func (g Grouping) arrange(prs []PR) []Group {
	byNumber := map[int]int{}
	for i, r := range g.Rules {
		for _, n := range r.PRs {
			if _, taken := byNumber[n]; !taken {
				byNumber[n] = i
			}
		}
	}

	ruled := make([][]PR, len(g.Rules))
	var rest []PR
	for _, p := range prs {
		if i, ok := byNumber[p.Number]; ok {
			ruled[i] = append(ruled[i], p)
			continue
		}
		if i := g.firstPatternMatch(p); i >= 0 {
			ruled[i] = append(ruled[i], p)
			continue
		}
		rest = append(rest, p)
	}

	var groups []Group
	for i, r := range g.Rules {
		if len(ruled[i]) > 0 {
			groups = append(groups, Group{Name: r.Name, PRs: newestFirst(ruled[i])})
		}
	}
	ungrouped := rest
	if g.Auto {
		var auto []Group
		auto, ungrouped = autoGroups(rest)
		groups = append(groups, auto...)
	}

	slices.SortStableFunc(groups, func(a, b Group) int {
		return newest(b.PRs).Compare(newest(a.PRs))
	})
	if len(ungrouped) > 0 {
		groups = append(groups, Group{Name: UngroupedName, PRs: newestFirst(ungrouped)})
	}
	return groups
}

func (g Grouping) firstPatternMatch(p PR) int {
	for i, r := range g.Rules {
		if (r.Branch != nil && r.Branch.MatchString(p.Branch)) || (r.Title != nil && r.Title.MatchString(p.Title)) {
			return i
		}
	}
	return -1
}

// newest is the creation time of the most recent PR; groups sort by it.
func newest(prs []PR) time.Time {
	var t time.Time
	for _, p := range prs {
		if p.CreatedAt.After(t) {
			t = p.CreatedAt
		}
	}
	return t
}

// autoGroups links PRs that share a ticket key or a branch family, and turns
// every linked set of two or more PRs into a group. PRs left on their own are
// returned as ungrouped.
func autoGroups(prs []PR) (groups []Group, alone []PR) {
	parent := make([]int, len(prs))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	owner := map[string]int{} // link key -> first PR index that had it
	keysOf := make([][]string, len(prs))
	for i, p := range prs {
		keysOf[i] = ticketKeys(p)
		links := append(slices.Clone(keysOf[i]), "branch:"+branchFamily(p.Branch))
		for _, k := range links {
			if j, ok := owner[k]; ok {
				parent[find(i)] = find(j)
			} else {
				owner[k] = i
			}
		}
	}

	members := map[int][]int{}
	var roots []int
	for i := range prs {
		r := find(i)
		if _, seen := members[r]; !seen {
			roots = append(roots, r)
		}
		members[r] = append(members[r], i)
	}
	for _, r := range roots {
		idx := members[r]
		if len(idx) < 2 {
			alone = append(alone, prs[idx[0]])
			continue
		}
		var set []PR
		var keys []string
		for _, i := range idx {
			set = append(set, prs[i])
			keys = append(keys, keysOf[i]...)
		}
		groups = append(groups, Group{Name: autoName(set, keys), PRs: newestFirst(set)})
	}
	return groups, alone
}

// autoName names a group after its oldest PR, led by its ticket keys.
func autoName(set []PR, keys []string) string {
	oldest := slices.MinFunc(set, func(a, b PR) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Number, b.Number)
	})
	slices.Sort(keys)
	keys = slices.Compact(keys)
	switch {
	case len(keys) == 0:
		return oldest.Title
	case len(keys) > 2:
		return fmt.Sprintf("%s +%d · %s", keys[0], len(keys)-1, oldest.Title)
	}
	return strings.Join(keys, ", ") + " · " + oldest.Title
}

var (
	// A key starts a branch path segment: "alice/abc-12-api", "feature/ABC-12/ui".
	// Two or more digits keep words like "utf-8" out.
	branchKey = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,9})-(\d{2,})`)
	// In titles a key is upper-case: "fix: crash (ABC-12)".
	titleKey = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9})-(\d{2,})\b`)
)

// ticketKeys returns the issue keys (e.g. "SEQ-1579") a PR mentions in its
// branch or title.
func ticketKeys(p PR) []string {
	var keys []string
	for seg := range strings.SplitSeq(p.Branch, "/") {
		if m := branchKey.FindStringSubmatch(seg); m != nil {
			keys = append(keys, strings.ToUpper(m[1])+"-"+m[2])
		}
	}
	for _, m := range titleKey.FindAllStringSubmatch(p.Title, -1) {
		keys = append(keys, m[1]+"-"+m[2])
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// branchFamily drops a "-split/<slice>" suffix, so the slices of a split PR
// share the original branch.
func branchFamily(branch string) string {
	if base, _, ok := strings.Cut(branch, "-split/"); ok {
		return base
	}
	return branch
}
