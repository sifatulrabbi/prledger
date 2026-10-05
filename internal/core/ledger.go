package core

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// SchemaVersion is the version of the Snapshot JSON contract. Bump it on any
// change that is not purely additive.
const SchemaVersion = 1

// Snapshot is everything a frontend needs to show one repo's PRs. It is the
// JSON contract shared by `list --json`, the HTTP API and the HTML export.
type Snapshot struct {
	Schema    int       `json:"schema"`
	Repo      string    `json:"repo"`
	Author    string    `json:"author"`
	FetchedAt time.Time `json:"fetchedAt"`
	Groups    []Group   `json:"groups"`
}

// Group is a named set of PRs that belong to one piece of work.
type Group struct {
	Name string `json:"name"`
	PRs  []PR   `json:"prs"`
}

// UngroupedName is the group that holds PRs no rule claimed.
const UngroupedName = "Ungrouped"

// Ledger builds Snapshots from a PRSource.
type Ledger struct {
	Source PRSource
	Now    func() time.Time
}

// Snapshot fetches the PRs for q and arranges them into groups.
func (l Ledger) Snapshot(ctx context.Context, q Query) (Snapshot, error) {
	prs, err := l.Source.ListPRs(ctx, q)
	if err != nil {
		return Snapshot{}, err
	}
	groups := []Group{}
	if len(prs) > 0 {
		groups = append(groups, Group{Name: UngroupedName, PRs: newestFirst(prs)})
	}
	return Snapshot{
		Schema:    SchemaVersion,
		Repo:      q.Repo.String(),
		Author:    q.Author,
		FetchedAt: l.Now().UTC(),
		Groups:    groups,
	}, nil
}

func newestFirst(prs []PR) []PR {
	out := slices.Clone(prs)
	slices.SortFunc(out, func(a, b PR) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.Number, a.Number)
	})
	return out
}
