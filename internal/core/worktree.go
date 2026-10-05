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

// extendWorktrees moves each leftover PR that shares a link (ticket key or
// branch family) with exactly one worktree's PRs into that worktree, and
// repeats so chains of links follow. Stacked branches are often created
// without being checked out, so the reflog alone misses them. A PR linked to
// two worktrees stays in rest.
func extendWorktrees(inWorktree [][]PR, rest []PR, keys keyFinder) ([][]PR, []PR) {
	owner := map[string]map[int]bool{} // link -> worktrees holding a PR with it
	claim := func(i int, p PR) {
		for _, link := range linksOf(p, keys.find(p)) {
			if owner[link] == nil {
				owner[link] = map[int]bool{}
			}
			owner[link][i] = true
		}
	}
	for i, prs := range inWorktree {
		for _, p := range prs {
			claim(i, p)
		}
	}
	for moved := true; moved; {
		moved = false
		var left []PR
		for _, p := range rest {
			reached := map[int]bool{}
			for _, link := range linksOf(p, keys.find(p)) {
				for i := range owner[link] {
					reached[i] = true
				}
			}
			if len(reached) != 1 {
				left = append(left, p)
				continue
			}
			for i := range reached {
				inWorktree[i] = append(inWorktree[i], p)
				claim(i, p)
			}
			moved = true
		}
		rest = left
	}
	return inWorktree, rest
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
			c.current == old.current && c.at.After(old.at):
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
