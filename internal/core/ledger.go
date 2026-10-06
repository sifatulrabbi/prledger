package core

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// SchemaVersion is the version of the Snapshot JSON contract. Bump it on any
// change that is not purely additive.
const SchemaVersion = 2 // 2: groups lost "worktree"; PRs gained "base", groups "stack"

// Snapshot is everything a frontend needs to show one repo's PRs. It is the
// JSON contract shared by `list --json`, the HTTP API and the HTML export.
type Snapshot struct {
	Schema        int       `json:"schema"`
	Repo          string    `json:"repo"`
	Author        string    `json:"author"`
	DefaultBranch string    `json:"defaultBranch,omitempty"` // "" in snapshots from before stacks
	FetchedAt     time.Time `json:"fetchedAt"`
	// Warnings name the extras gh could not give (default branch, reviews,
	// CI); the PRs are complete without them.
	Warnings []string `json:"warnings,omitempty"`
	Groups   []Group  `json:"groups"`
}

// Group is a named set of PRs that belong to one piece of work.
type Group struct {
	Name  string `json:"name"`
	Stack *Stack `json:"stack,omitempty"` // set when the PRs are stacked; PRs are then in stack order
	PRs   []PR   `json:"prs"`
}

// UngroupedName is the group that holds PRs no rule claimed.
const UngroupedName = "Ungrouped"

// Ledger builds Snapshots from a PRSource.
type Ledger struct {
	Source   PRSource
	Now      func() time.Time
	Grouping Grouping
}

// Snapshot fetches the PRs for q and arranges them into groups.
func (l Ledger) Snapshot(ctx context.Context, q Query) (Snapshot, error) {
	prs, err := l.Source.ListPRs(ctx, q)
	if err != nil {
		return Snapshot{}, err
	}
	var warnings []string
	// Without the default branch only shared-base stacks are lost, so a
	// failure here does not fail the snapshot.
	base, err := l.Source.DefaultBranch(ctx, q.Repo)
	if err != nil {
		base = ""
		warnings = append(warnings, "Stacks on shared branches are not shown: "+err.Error())
	}
	prs, err = l.withDetails(ctx, q, prs)
	if err != nil {
		warnings = append(warnings, "Reviews, CI and merge state are not shown: "+err.Error())
	}
	return Snapshot{
		Schema:        SchemaVersion,
		Repo:          q.Repo.String(),
		Author:        q.Author,
		DefaultBranch: base,
		FetchedAt:     l.Now().UTC(),
		Warnings:      warnings,
		Groups:        orEmpty(l.Grouping.arrange(prs, base)),
	}, nil
}

// withDetails adds the open PRs' details to prs. It skips the call when no
// PR is open, and on failure returns prs unchanged with the error.
func (l Ledger) withDetails(ctx context.Context, q Query, prs []PR) ([]PR, error) {
	if !slices.ContainsFunc(prs, func(p PR) bool { return p.Status == StatusOpen || p.Status == StatusDraft }) {
		return prs, nil
	}
	details, err := l.Source.OpenDetails(ctx, q)
	if err != nil && ctx.Err() == nil {
		// GitHub sometimes cuts this slow response short; a second try
		// usually works.
		details, err = l.Source.OpenDetails(ctx, q)
	}
	if err != nil {
		return prs, err
	}
	byNumber := make(map[int]Details, len(details))
	for _, d := range details {
		byNumber[d.Number] = d
	}
	out := make([]PR, len(prs))
	for i, p := range prs {
		if d, ok := byNumber[p.Number]; ok {
			p = d.apply(p)
		}
		out[i] = p
	}
	return out, nil
}

// Regroup re-arranges a snapshot's PRs with the Ledger's grouping, e.g. a
// cached snapshot after the config changed. It does not fetch.
func (l Ledger) Regroup(s Snapshot) Snapshot {
	s.Groups = orEmpty(l.Grouping.arrange(s.PRs(), s.DefaultBranch))
	return s
}

// PRs returns every PR in the snapshot.
func (s Snapshot) PRs() []PR {
	var prs []PR
	for _, g := range s.Groups {
		prs = append(prs, g.PRs...)
	}
	return prs
}

func orEmpty(groups []Group) []Group {
	if groups == nil {
		return []Group{} // JSON [] rather than null
	}
	return groups
}

// olderFirst orders PRs by creation time, then number.
func olderFirst(a, b PR) int {
	if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
		return c
	}
	return cmp.Compare(a.Number, b.Number)
}

func newestFirst(prs []PR) []PR {
	out := slices.Clone(prs)
	slices.SortFunc(out, func(a, b PR) int { return olderFirst(b, a) })
	return out
}
