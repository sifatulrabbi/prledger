package core

import (
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

// Grouping decides which group each PR lands in. Precedence: the stack the PR
// belongs to, then a rule that lists the PR's number, then the first rule
// whose pattern matches, then (if Auto) automatic grouping, then Ungrouped.
type Grouping struct {
	Rules []GroupRule
	Auto  bool
	// TicketPrefixes, when set, are the only ticket key prefixes automatic
	// grouping recognises (e.g. "SEQ" for SEQ-123). Unset means guess.
	TicketPrefixes []string
}

// Suggest runs automatic grouping over the PRs that are not stacked and that
// no rule claims, whether or not Auto is on. It returns the linked groups
// (newest first) and the PRs left on their own.
func (g Grouping) Suggest(prs []PR, defaultBranch string) (groups []Group, alone []PR) {
	keys := newKeyFinder(g.TicketPrefixes)
	_, loose := stacks(prs, defaultBranch)
	_, rest := g.split(loose)
	groups, alone = autoGroups(rest, keys)
	sortGroups(groups)
	return groups, newestFirst(alone)
}

// split applies everything before automatic grouping and returns those
// groups and the PRs left over. arrange and Suggest share it so they always
// agree on what is left.
func (g Grouping) split(prs []PR) (claimed []Group, rest []PR) {
	byNumber := map[int]int{}
	for i, r := range g.Rules {
		for _, n := range r.PRs {
			if _, taken := byNumber[n]; !taken {
				byNumber[n] = i
			}
		}
	}
	ruled := make([][]PR, len(g.Rules))
	for _, p := range prs {
		if i, ok := byNumber[p.Number]; ok {
			ruled[i] = append(ruled[i], p)
		} else if i := g.firstPatternMatch(p); i >= 0 {
			ruled[i] = append(ruled[i], p)
		} else {
			rest = append(rest, p)
		}
	}
	for i, r := range g.Rules {
		if len(ruled[i]) > 0 {
			claimed = append(claimed, Group{Name: r.Name, PRs: newestFirst(ruled[i])})
		}
	}
	return claimed, rest
}

func (g Grouping) arrange(prs []PR, defaultBranch string) []Group {
	keys := newKeyFinder(g.TicketPrefixes)
	groups, loose := stacks(prs, defaultBranch)
	claimed, ungrouped := g.split(loose)
	groups = append(groups, claimed...)
	if g.Auto {
		var auto []Group
		auto, ungrouped = autoGroups(ungrouped, keys)
		groups = append(groups, auto...)
	}
	// Names are made unique in precedence order, so config rules keep theirs;
	// "Ungrouped" is reserved for the real Ungrouped group.
	taken := map[string]bool{UngroupedName: true}
	for i := range groups {
		groups[i].Name = UniqueName(groups[i].Name, taken)
	}
	sortGroups(groups)
	if len(ungrouped) > 0 {
		groups = append(groups, Group{Name: UngroupedName, PRs: newestFirst(ungrouped)})
	}
	return groups
}

// sortGroups orders groups by their newest PR, then by name, so the order is
// the same whatever order the PRs came in.
func sortGroups(groups []Group) {
	slices.SortFunc(groups, func(a, b Group) int {
		if c := newest(b.PRs).Compare(newest(a.PRs)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// UniqueName returns name, or name with " (2)", " (3)"… if it is taken, and
// marks the result taken.
func UniqueName(name string, taken map[string]bool) string {
	candidate := name
	for n := 2; taken[candidate]; n++ {
		candidate = fmt.Sprintf("%s (%d)", name, n)
	}
	taken[candidate] = true
	return candidate
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
func autoGroups(prs []PR, keys keyFinder) (groups []Group, alone []PR) {
	for _, set := range linkedSets(prs, keys) {
		if len(set.prs) < 2 {
			alone = append(alone, set.prs[0])
			continue
		}
		groups = append(groups, Group{Name: autoName(set.prs, set.keys), PRs: newestFirst(set.prs)})
	}
	return groups, alone
}

// linkedSet is PRs joined by shared links, with each PR's links and all the
// ticket keys in the set.
type linkedSet struct {
	prs   []PR
	links map[int][]string // PR number -> its links
	keys  []string
}

// linkedSets splits prs into sets of PRs that share a ticket key or branch
// family, directly or through other PRs (union-find). Which PRs end up
// together does not depend on their order.
func linkedSets(prs []PR, keys keyFinder) []linkedSet {
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
	firstWith := map[string]int{} // link -> index of the first PR that had it
	keysOf := make([][]string, len(prs))
	linksOfPR := make([][]string, len(prs))
	for i, p := range prs {
		keysOf[i] = keys.find(p)
		linksOfPR[i] = linksOf(p, keysOf[i])
		for _, link := range linksOfPR[i] {
			if j, ok := firstWith[link]; ok {
				parent[find(i)] = find(j)
			} else {
				firstWith[link] = i
			}
		}
	}

	byRoot := map[int]int{} // root -> index into sets
	var sets []linkedSet
	for i, p := range prs {
		r := find(i)
		at, seen := byRoot[r]
		if !seen {
			at = len(sets)
			byRoot[r] = at
			sets = append(sets, linkedSet{links: map[int][]string{}})
		}
		s := &sets[at]
		s.prs = append(s.prs, p)
		s.links[p.Number] = linksOfPR[i]
		s.keys = append(s.keys, keysOf[i]...)
	}
	return sets
}

// autoName names a group after its oldest PR, led by its ticket keys.
func autoName(set []PR, keys []string) string {
	oldest := slices.MinFunc(set, olderFirst)
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
	// Guessed keys start a branch path segment: "alice/abc-12-api",
	// "feature/ABC-12/ui". Two or more digits keep words like "utf-8" out.
	branchKey = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,9})-(\d{2,})`)
	// In titles a guessed key is upper-case: "fix: crash (ABC-12)".
	titleKey = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9})-(\d{2,})\b`)
)

// notTicketPrefixes are words that often precede a number in branches and
// titles without being an issue tracker: commit types, versions, encodings,
// hashes and standards.
var notTicketPrefixes = map[string]bool{
	"FIX": true, "FEAT": true, "FEATURE": true, "BUG": true, "BUGFIX": true, "HOTFIX": true,
	"CHORE": true, "DOCS": true, "DOC": true, "TEST": true, "TESTS": true, "REFACTOR": true,
	"REF": true, "PERF": true, "WIP": true, "UPDATE": true, "UPGRADE": true, "BUMP": true,
	"RELEASE": true, "VERSION": true, "V": true, "SHA": true, "MD": true, "UTF": true,
	"UCS": true, "ISO": true, "AES": true, "RSA": true, "HTTP": true, "TLS": true,
	"SSL": true, "RFC": true, "ES": true, "ECMA": true, "IPV": true, "WIN": true,
}

// keyFinder finds the ticket keys (e.g. "ABC-123") a PR mentions in its
// branch or title.
type keyFinder struct {
	only *regexp.Regexp // set when the repo lists its ticket prefixes
}

func newKeyFinder(prefixes []string) keyFinder {
	if len(prefixes) == 0 {
		return keyFinder{}
	}
	quoted := make([]string, len(prefixes))
	for i, p := range prefixes {
		quoted[i] = regexp.QuoteMeta(p)
	}
	return keyFinder{only: regexp.MustCompile(`(?i)\b(` + strings.Join(quoted, "|") + `)-(\d+)`)}
}

func (k keyFinder) find(p PR) []string {
	var keys []string
	add := func(prefix, num string) {
		prefix = strings.ToUpper(prefix)
		if k.only != nil || !notTicketPrefixes[prefix] {
			keys = append(keys, prefix+"-"+num)
		}
	}
	if k.only != nil {
		for _, text := range []string{p.Branch, p.Title} {
			for _, m := range k.only.FindAllStringSubmatch(text, -1) {
				add(m[1], m[2])
			}
		}
	} else {
		for seg := range strings.SplitSeq(p.Branch, "/") {
			if m := branchKey.FindStringSubmatch(seg); m != nil {
				add(m[1], m[2])
			}
		}
		for _, m := range titleKey.FindAllStringSubmatch(p.Title, -1) {
			add(m[1], m[2])
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// linksOf is what ties a PR to others in automatic grouping: its ticket keys
// and its branch family.
func linksOf(p PR, ticketKeys []string) []string {
	return append(slices.Clone(ticketKeys), "branch:"+branchFamily(p.Branch))
}

// branchFamily drops a "-split/<slice>" suffix, so the slices of a split PR
// share the original branch.
func branchFamily(branch string) string {
	if base, _, ok := strings.Cut(branch, "-split/"); ok {
		return base
	}
	return branch
}
