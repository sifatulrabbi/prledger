package core

import "slices"

// Label is a GitHub label on a PR.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"` // hex without "#", as GitHub gives it
	Description string `json:"description,omitempty"`
}

// ReviewerState is where one reviewer stands on a PR.
type ReviewerState string

const (
	ReviewerRequested ReviewerState = "requested"
	ReviewerApproved  ReviewerState = "approved"
	ReviewerChanges   ReviewerState = "changes_requested"
	ReviewerCommented ReviewerState = "commented"
)

// Reviewer is a user or team (as "org/team") asked to review, or who did.
type Reviewer struct {
	Login string        `json:"login"`
	State ReviewerState `json:"state"`
}

// Review is a PR's overall review state.
type Review string

const (
	ReviewApproved Review = "approved"
	ReviewChanges  Review = "changes_requested"
	ReviewRequired Review = "review_required"
)

// CheckState is the result of one CI check, or of all of them.
type CheckState string

const (
	CheckPass    CheckState = "pass"
	CheckFail    CheckState = "fail"
	CheckPending CheckState = "pending"
)

// Checks rolls up a PR's CI checks.
type Checks struct {
	State   CheckState `json:"state"`
	Passed  int        `json:"passed"` // skipped and neutral checks count as passed
	Failed  int        `json:"failed"`
	Pending int        `json:"pending"`
}

// SummarizeChecks rolls up check results: any failure fails, else anything
// pending is pending. It returns nil when there are no checks.
func SummarizeChecks(states []CheckState) *Checks {
	if len(states) == 0 {
		return nil
	}
	c := &Checks{State: CheckPass}
	for _, s := range states {
		switch s {
		case CheckFail:
			c.Failed++
		case CheckPending:
			c.Pending++
		default:
			c.Passed++
		}
	}
	if c.Failed > 0 {
		c.State = CheckFail
	} else if c.Pending > 0 {
		c.State = CheckPending
	}
	return c
}

// Merge says whether a PR can merge into its base. "" means GitHub has not
// worked it out yet, which is common: it computes it lazily.
type Merge string

const (
	MergeClean       Merge = "clean"
	MergeConflicting Merge = "conflicting"
	MergeBehind      Merge = "behind"  // the base moved on and the branch must be updated
	MergeBlocked     Merge = "blocked" // a rule (review, checks) blocks it
)

// Attention says whether an open PR needs its author.
type Attention string

const (
	AttentionNeedsYou Attention = "needs_you" // conflicts, failing CI or requested changes
	AttentionApproved Attention = "approved"
	AttentionWaiting  Attention = "waiting" // on reviewers or CI
)

// Details is what GitHub knows about an open PR beyond the list fields.
// Fetching them is slow, so they come only for open PRs, in a second call.
type Details struct {
	Number    int
	Author    string     // the PR's author; their own replies are not reviews
	Decision  Review     // GitHub's review decision; "" when the repo requires no review
	Requested []string   // reviewers asked and not yet answered
	Reviews   []Reviewer // each reviewer's latest review, oldest first
	Checks    *Checks
	Merge     Merge
}

// apply adds d to p and works out p's review state and attention.
func (d Details) apply(p PR) PR {
	p.Reviewers = d.reviewers()
	p.Review = d.Decision
	if p.Review == "" {
		p.Review = reviewOf(p.Reviewers)
	}
	p.Checks = d.Checks
	p.Merge = d.Merge
	p.Attention = attentionOf(p)
	return p
}

// reviewers lists everyone who reviewed, then everyone still asked. Asking a
// reviewer again makes them pending, whatever they said before.
func (d Details) reviewers() []Reviewer {
	var out []Reviewer
	for _, r := range d.Reviews {
		if r.Login == d.Author || slices.Contains(d.Requested, r.Login) {
			continue
		}
		out = append(out, r)
	}
	for _, login := range d.Requested {
		out = append(out, Reviewer{Login: login, State: ReviewerRequested})
	}
	return out
}

// reviewOf stands in for GitHub's decision in repos that require no review.
func reviewOf(rs []Reviewer) Review {
	has := func(s ReviewerState) bool {
		return slices.ContainsFunc(rs, func(r Reviewer) bool { return r.State == s })
	}
	switch {
	case has(ReviewerChanges):
		return ReviewChanges
	case has(ReviewerApproved):
		return ReviewApproved
	case has(ReviewerRequested):
		return ReviewRequired
	}
	return ""
}

func attentionOf(p PR) Attention {
	if p.Status != StatusOpen && p.Status != StatusDraft {
		return ""
	}
	if p.Merge == MergeConflicting || (p.Checks != nil && p.Checks.State == CheckFail) || p.Review == ReviewChanges {
		return AttentionNeedsYou
	}
	switch {
	case p.Status == StatusDraft:
		return "" // still the author's work in progress
	case p.Review == ReviewApproved:
		return AttentionApproved
	}
	return AttentionWaiting
}
