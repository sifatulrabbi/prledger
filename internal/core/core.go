// Package core holds prledger's domain types and the Ledger service. It has no
// knowledge of gh, git, files or HTTP; adapters plug in through its ports.
package core

import (
	"context"
	"time"
)

// Repo identifies a GitHub repository.
type Repo struct {
	Owner string
	Name  string
}

// String returns "owner/name", the form gh's --repo flag takes.
func (r Repo) String() string { return r.Owner + "/" + r.Name }

// Status is where a pull request stands. Draft is an open PR marked as draft.
type Status string

const (
	StatusOpen   Status = "open"
	StatusDraft  Status = "draft"
	StatusMerged Status = "merged"
	StatusClosed Status = "closed"
)

// PR is one pull request as prledger shows it.
type PR struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Branch    string     `json:"branch"`
	Base      string     `json:"base"` // the branch it merges into; "" in snapshots from before stacks
	Status    Status     `json:"status"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"createdAt"`
	MergedAt  *time.Time `json:"mergedAt"`
	ClosedAt  *time.Time `json:"closedAt"`
}

// Query selects the pull requests to list.
type Query struct {
	Repo   Repo
	Author string // a GitHub login, or "@me" for the gh user
	Limit  int
}

// PRSource is the port to wherever pull requests come from (gh in practice).
type PRSource interface {
	ListPRs(ctx context.Context, q Query) ([]PR, error)
	// DefaultBranch is the branch the repo merges into by default; PRs based
	// on any other branch that is not a PR of theirs are on a shared base.
	DefaultBranch(ctx context.Context, repo Repo) (string, error)
}
