package core

import "slices"

// Stack describes a group of stacked PRs: PRs whose base branch is another
// PR's head branch, or that share a base branch other than the default one.
type Stack struct {
	// Base is the shared branch the stack sits on; "" when it sits on the
	// default branch.
	Base string `json:"base,omitempty"`
	// Entries has one entry per PR, in the same order as the group's PRs:
	// bottom first, each PR followed by the PRs stacked on it.
	Entries []StackEntry `json:"entries"`
}

// StackEntry places one PR in its stack.
type StackEntry struct {
	Number int `json:"number"`
	On     int `json:"on,omitempty"` // the PR it sits on; 0 at the bottom
	Depth  int `json:"depth"`
}

// stacks pulls the stacked PRs out of prs and returns them as groups, plus
// the PRs that are not stacked. defaultBranch may be "" when unknown, in which
// case only PR-on-PR stacking is detected. The result does not depend on the
// order of prs.
func stacks(prs []PR, defaultBranch string) (groups []Group, rest []PR) {
	ord := slices.Clone(prs)
	slices.SortFunc(ord, olderFirst)
	n := len(ord)

	// Empty names never link: snapshots from before stacks have no base.
	heads := map[string][]int{} // head branch -> PRs with it, oldest first
	for i, p := range ord {
		if p.Branch != "" {
			heads[p.Branch] = append(heads[p.Branch], i)
		}
	}
	parent := make([]int, n)
	for i, p := range ord {
		parent[i] = -1
		if p.Base != "" {
			parent[i] = parentOf(i, heads[p.Base])
		}
	}
	shared := func(i int) string {
		if parent[i] >= 0 || defaultBranch == "" || ord[i].Base == "" || ord[i].Base == defaultBranch {
			return ""
		}
		return ord[i].Base
	}

	uf := newUnionFind(n)
	firstOnBase := map[string]int{}
	for i := range ord {
		if parent[i] >= 0 {
			uf.union(i, parent[i])
		}
		if b := shared(i); b != "" {
			if j, ok := firstOnBase[b]; ok {
				uf.union(i, j)
			} else {
				firstOnBase[b] = i
			}
		}
	}
	// A reused head branch is the same work, but only counts inside a stack.
	for _, same := range heads {
		for _, i := range same[1:] {
			uf.union(i, same[0])
		}
	}

	byRoot := map[int][]int{}
	var roots []int
	for i := range ord {
		r := uf.find(i)
		if _, seen := byRoot[r]; !seen {
			roots = append(roots, r)
		}
		byRoot[r] = append(byRoot[r], i)
	}
	for _, r := range roots {
		members := byRoot[r]
		base, linked := "", false
		for _, i := range members {
			if parent[i] >= 0 {
				linked = true
			}
			if b := shared(i); b != "" && base == "" {
				base = b
			}
		}
		if !linked && base == "" {
			for _, i := range members {
				rest = append(rest, ord[i])
			}
			continue
		}
		groups = append(groups, walkStack(ord, members, parent, base))
	}
	return groups, rest
}

// parentOf picks the PR that i sits on among the PRs whose head is i's base:
// the newest one created before i, else the oldest one after it.
func parentOf(i int, candidates []int) int {
	best := -1
	for _, c := range candidates { // oldest first, by index in the sorted list
		if c == i {
			continue
		}
		if c < i {
			best = c // keeps the newest before i
		} else if best < 0 {
			return c
		}
	}
	return best
}

// walkStack orders a stack's members bottom first, each PR followed by the
// PRs stacked on it, oldest first. A loop of bases has no bottom; its oldest
// member is treated as one, so every member is placed exactly once.
func walkStack(ord []PR, members, parent []int, base string) Group {
	in := map[int]bool{}
	for _, i := range members {
		in[i] = true
	}
	children := map[int][]int{}
	for _, i := range members { // members are oldest first, so children are too
		if p := parent[i]; p >= 0 && in[p] {
			children[p] = append(children[p], i)
		}
	}
	g := Group{Stack: &Stack{Base: base}}
	placed := map[int]bool{}
	var walk func(i, on, depth int)
	walk = func(i, on, depth int) {
		if placed[i] {
			return
		}
		placed[i] = true
		g.PRs = append(g.PRs, ord[i])
		g.Stack.Entries = append(g.Stack.Entries, StackEntry{Number: ord[i].Number, On: on, Depth: depth})
		for _, c := range children[i] {
			walk(c, ord[i].Number, depth+1)
		}
	}
	for _, i := range members {
		if p := parent[i]; p < 0 || !in[p] {
			walk(i, 0, 0)
		}
	}
	for _, i := range members { // only members of a loop are left
		walk(i, 0, 0)
	}

	if base != "" {
		g.Name = "on " + base
	} else {
		g.Name = g.PRs[0].Title
	}
	return g
}

type unionFind []int

func newUnionFind(n int) unionFind {
	uf := make(unionFind, n)
	for i := range uf {
		uf[i] = i
	}
	return uf
}

func (uf unionFind) find(i int) int {
	if uf[i] != i {
		uf[i] = uf.find(uf[i])
	}
	return uf[i]
}

func (uf unionFind) union(a, b int) { uf[uf.find(a)] = uf.find(b) }
