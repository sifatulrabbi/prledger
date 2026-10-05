package core

import "time"

// Worktree is a local git worktree of the repo and the branches it has had
// checked out. In a worktree-per-feature workflow it is one stream of work.
type Worktree struct {
	Name     string               // folder name; the group is named after it
	Path     string               // shown on the group
	Branches map[string]time.Time // branch -> when it was last checked out here
	Current  string               // branch checked out now, "" when detached
}

// worktreeOwners maps each branch to the index of the worktree that owns it:
// the worktree where it is checked out now, else the one that checked it out
// most recently. Ties go to the earlier worktree.
func worktreeOwners(wts []Worktree) map[string]int {
	type claim struct {
		idx     int
		current bool
		at      time.Time
	}
	best := map[string]claim{}
	consider := func(branch string, c claim) {
		old, seen := best[branch]
		switch {
		case !seen,
			c.current && !old.current,
			c.current == old.current && c.at.After(old.at),
			c.current == old.current && c.at.Equal(old.at) && c.idx < old.idx:
			best[branch] = c
		}
	}
	for i, w := range wts {
		for b, at := range w.Branches {
			consider(b, claim{idx: i, at: at})
		}
		if w.Current != "" {
			consider(w.Current, claim{idx: i, current: true})
		}
	}
	owners := make(map[string]int, len(best))
	for b, c := range best {
		owners[b] = c.idx
	}
	return owners
}

// extendWorktrees moves leftover PRs into worktrees they are linked to.
// Stacked branches are often created without being checked out, so the
// reflog alone misses them. Leftover PRs are first joined into linked sets
// (shared ticket key or branch family, as in automatic grouping); a set joins
// a worktree when its links reach exactly that one, through the branches the
// worktree owns or the PRs it already holds. Working on whole sets keeps the
// result independent of PR order. A set reaching two worktrees stays in rest.
func extendWorktrees(owners map[string]int, inWorktree [][]PR, rest []PR, keys keyFinder) ([][]PR, []PR) {
	reaches := map[string]map[int]bool{} // link -> worktrees it leads to
	mark := func(links []string, i int) {
		for _, l := range links {
			if reaches[l] == nil {
				reaches[l] = map[int]bool{}
			}
			reaches[l][i] = true
		}
	}
	for branch, i := range owners {
		b := PR{Branch: branch}
		mark(linksOf(b, keys.find(b)), i)
	}
	for i, prs := range inWorktree {
		for _, p := range prs {
			mark(linksOf(p, keys.find(p)), i)
		}
	}

	var left []PR
	for _, set := range linkedSets(rest, keys) {
		reached := map[int]bool{}
		for _, p := range set.prs {
			for _, l := range set.links[p.Number] {
				for i := range reaches[l] {
					reached[i] = true
				}
			}
		}
		if len(reached) != 1 {
			left = append(left, set.prs...)
			continue
		}
		for i := range reached {
			inWorktree[i] = append(inWorktree[i], set.prs...)
		}
	}
	return inWorktree, left
}
