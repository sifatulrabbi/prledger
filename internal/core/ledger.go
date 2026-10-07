package core

import (
	"cmp"
	"context"
	"fmt"
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
	// SkipDetails leaves out reviews, CI and merge state (and their slow
	// call), for callers that only need titles and branches.
	SkipDetails bool
	// DetailsTimeout bounds each try of the details call; 0 means
	// DefaultDetailsTimeout. Two tries fit in serve's DefaultFetchTimeout.
	DetailsTimeout time.Duration
}

// DefaultDetailsTimeout is how long one try of the details call may take.
const DefaultDetailsTimeout = 40 * time.Second

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
	if !l.SkipDetails && slices.ContainsFunc(prs, live) {
		var w []string
		prs, w = l.withDetails(ctx, q, prs)
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err() // cancelled: no half snapshot for the cache
		}
		warnings = append(warnings, w...)
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

func live(p PR) bool { return p.Status == StatusOpen || p.Status == StatusDraft }

// withDetails adds the open PRs' details and discussions to prs. The two
// calls run side by side; each one that fails leaves its part out and says so
// in a warning.
func (l Ledger) withDetails(ctx context.Context, q Query, prs []PR) ([]PR, []string) {
	limit := cmp.Or(l.DetailsTimeout, DefaultDetailsTimeout)
	var talk []Discussion
	var talkErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		talk, talkErr = fetchExtra(ctx, limit, func(ctx context.Context) ([]Discussion, error) { return l.Source.Discussions(ctx, q) })
	}()
	details, err := fetchExtra(ctx, limit, func(ctx context.Context) ([]Details, error) { return l.Source.OpenDetails(ctx, q) })
	<-done

	var warnings []string
	if err != nil {
		warnings = append(warnings, "Reviews, CI and merge state are not shown: "+err.Error())
	}
	if talkErr != nil {
		warnings = append(warnings, "Review comments are not shown: "+talkErr.Error())
	}
	detailsOf := byNumber(details, func(d Details) int { return d.Number })
	talkOf := byNumber(talk, func(t Discussion) int { return t.Number })
	out := make([]PR, len(prs))
	for i, p := range prs {
		if t, ok := talkOf[p.Number]; ok {
			p = t.apply(p)
		}
		// Attention weighs CI and reviews first; comments alone cannot tell
		// whether a PR waits on others.
		if d, ok := detailsOf[p.Number]; ok {
			p = d.apply(p)
			p.Attention = attentionOf(p)
		}
		out[i] = p
	}
	return out, warnings
}

func byNumber[T any](items []T, number func(T) int) map[int]T {
	m := make(map[int]T, len(items))
	for _, it := range items {
		m[number(it)] = it
	}
	return m
}

// fetchExtra runs one of the optional calls. Each try gets its own time, so a
// hung call cannot use up the whole fetch's time and leave none for a second
// try; GitHub sometimes cuts these slow responses short, and a second try
// usually works.
func fetchExtra[T any](ctx context.Context, limit time.Duration, fetch func(context.Context) ([]T, error)) ([]T, error) {
	try := func() ([]T, error) {
		tctx, cancel := context.WithTimeout(ctx, limit)
		defer cancel()
		out, err := fetch(tctx)
		if err != nil && ctx.Err() == nil && tctx.Err() != nil {
			return nil, fmt.Errorf("gh took longer than %s", limit)
		}
		return out, err
	}
	out, err := try()
	if err != nil && ctx.Err() == nil {
		out, err = try()
	}
	return out, err
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
