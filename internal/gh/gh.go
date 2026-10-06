// Package gh is the PRSource adapter backed by the GitHub CLI. prledger never
// talks to GitHub itself: gh does the network calls and owns authentication.
package gh

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// Runner runs one process and returns its stdout. env holds extra KEY=VALUE
// entries added on top of the parent environment.
type Runner interface {
	Run(ctx context.Context, argv, env []string) ([]byte, error)
}

// Client lists pull requests by running gh.
type Client struct {
	Runner  Runner
	Command []string          // how to start gh, e.g. ["gh"] or ["env", "GH_CONFIG_DIR=…", "gh"]
	Env     map[string]string // extra environment for gh
}

// prFields are cheap for GitHub to answer even for hundreds of PRs.
const prFields = "number,title,headRefName,baseRefName,state,isDraft,url,createdAt,mergedAt,closedAt,author,labels,assignees"

// detailFields are not: asking for them across every PR of a busy repo makes
// GitHub time out (HTTP 502/504), so OpenDetails asks for open PRs only.
const detailFields = "number,reviewDecision,reviewRequests,latestReviews,statusCheckRollup,mergeable,mergeStateStatus"

type ghPR struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	HeadRefName string     `json:"headRefName"`
	BaseRefName string     `json:"baseRefName"`
	State       string     `json:"state"`
	IsDraft     bool       `json:"isDraft"`
	URL         string     `json:"url"`
	CreatedAt   time.Time  `json:"createdAt"`
	MergedAt    *time.Time `json:"mergedAt"`
	ClosedAt    *time.Time `json:"closedAt"`
	Labels      []struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	} `json:"labels"`
	Author    ghUser   `json:"author"`
	Assignees []ghUser `json:"assignees"`
}

type ghUser struct {
	Login string `json:"login"`
}

type ghDetails struct {
	Number         int    `json:"number"`
	ReviewDecision string `json:"reviewDecision"`
	ReviewRequests []struct {
		Login string `json:"login"` // users and bots
		Slug  string `json:"slug"`  // teams, as "org/team"
		Name  string `json:"name"`
	} `json:"reviewRequests"`
	LatestReviews     []ghReview `json:"latestReviews"`
	StatusCheckRollup []struct {
		Status     string `json:"status"`     // check runs
		Conclusion string `json:"conclusion"` // check runs, once completed
		State      string `json:"state"`      // commit statuses
	} `json:"statusCheckRollup"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
}

type ghReview struct {
	Author      ghUser    `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// Exec runs gh with args and returns its stdout.
func (c Client) Exec(ctx context.Context, args ...string) ([]byte, error) {
	if len(c.Command) == 0 {
		return nil, errors.New("gh_command is empty")
	}
	return c.Runner.Run(ctx, append(slices.Clone(c.Command), args...), EnvList(c.Env))
}

// CommandLine is the shell command that runs gh with args the way Exec does,
// quoted so it can be pasted into a shell (for hints).
func (c Client) CommandLine(args ...string) string {
	words := append(EnvList(c.Env), c.Command...)
	words = append(words, args...)
	for i, w := range words {
		if k, v, ok := strings.Cut(w, "="); ok && i < len(c.Env) {
			words[i] = k + "=" + shellQuote(v)
		} else {
			words[i] = shellQuote(w)
		}
	}
	return strings.Join(words, " ")
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./~-]+$`)

func shellQuote(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// list runs `gh pr list` for q in one state and decodes the JSON into out.
func (c Client) list(ctx context.Context, q core.Query, state, fields string, out any) error {
	raw, err := c.Exec(ctx,
		"pr", "list",
		"--repo", q.Repo.String(),
		"--author", q.Author,
		"--state", state,
		"--limit", strconv.Itoa(q.Limit),
		"--json", fields,
	)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("reading gh pr list output: %w", err)
	}
	return nil
}

// ListPRs runs `gh pr list` for every state and maps the result.
func (c Client) ListPRs(ctx context.Context, q core.Query) ([]core.PR, error) {
	var raw []ghPR
	if err := c.list(ctx, q, "all", prFields, &raw); err != nil {
		return nil, fmt.Errorf("listing pull requests with gh: %w", err)
	}
	prs := make([]core.PR, 0, len(raw))
	for _, r := range raw {
		status, err := toStatus(r.State, r.IsDraft)
		if err != nil {
			return nil, fmt.Errorf("PR #%d: %w", r.Number, err)
		}
		p := core.PR{
			Number:    r.Number,
			Title:     r.Title,
			Branch:    r.HeadRefName,
			Base:      r.BaseRefName,
			Status:    status,
			URL:       r.URL,
			CreatedAt: r.CreatedAt,
			MergedAt:  r.MergedAt,
			ClosedAt:  r.ClosedAt,
			Author:    r.Author.Login,
		}
		for _, l := range r.Labels {
			p.Labels = append(p.Labels, core.Label{Name: l.Name, Color: l.Color, Description: l.Description})
		}
		for _, a := range r.Assignees {
			p.Assignees = append(p.Assignees, a.Login)
		}
		prs = append(prs, p)
	}
	return prs, nil
}

// OpenDetails runs `gh pr list` for open PRs with the slow fields.
func (c Client) OpenDetails(ctx context.Context, q core.Query) ([]core.Details, error) {
	var raw []ghDetails
	if err := c.list(ctx, q, "open", detailFields, &raw); err != nil {
		return nil, fmt.Errorf("reading reviews and checks with gh: %w", err)
	}
	out := make([]core.Details, 0, len(raw))
	for _, r := range raw {
		d := core.Details{
			Number:   r.Number,
			Decision: reviewDecision[r.ReviewDecision],
			Merge:    mergeState(r.Mergeable, r.MergeStateStatus),
		}
		for _, rr := range r.ReviewRequests {
			if who := cmp.Or(rr.Login, rr.Slug, rr.Name); who != "" {
				d.Requested = append(d.Requested, who)
			}
		}
		reviews := slices.Clone(r.LatestReviews)
		slices.SortStableFunc(reviews, func(a, b ghReview) int { return a.SubmittedAt.Compare(b.SubmittedAt) })
		for _, rv := range reviews {
			if s, ok := reviewerState[rv.State]; ok && rv.Author.Login != "" {
				d.Reviews = append(d.Reviews, core.Reviewer{Login: rv.Author.Login, State: s})
			}
		}
		var checks []core.CheckState
		for _, ch := range r.StatusCheckRollup {
			checks = append(checks, checkState(ch.Status, ch.Conclusion, ch.State))
		}
		d.Checks = core.SummarizeChecks(checks)
		out = append(out, d)
	}
	return out, nil
}

var reviewDecision = map[string]core.Review{
	"APPROVED":          core.ReviewApproved,
	"CHANGES_REQUESTED": core.ReviewChanges,
	"REVIEW_REQUIRED":   core.ReviewRequired,
}

// reviewerState maps the review states that take a stand. Comment-only
// reviews do not (and bots such as github-actions leave one on most PRs);
// dismissed and unsubmitted (PENDING) reviews say nothing either.
var reviewerState = map[string]core.ReviewerState{
	"APPROVED":          core.ReviewerApproved,
	"CHANGES_REQUESTED": core.ReviewerChanges,
}

// checkState reads one statusCheckRollup entry: a check run (status and
// conclusion) or a commit status (state).
func checkState(status, conclusion, state string) core.CheckState {
	if state != "" { // commit status
		switch state {
		case "SUCCESS":
			return core.CheckPass
		case "PENDING", "EXPECTED":
			return core.CheckPending
		}
		return core.CheckFail // FAILURE, ERROR
	}
	if status != "COMPLETED" {
		return core.CheckPending // QUEUED, IN_PROGRESS, WAITING, …
	}
	switch conclusion {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return core.CheckPass
	}
	return core.CheckFail // FAILURE, CANCELLED, TIMED_OUT, ACTION_REQUIRED, STALE, …
}

// mergeState reads GitHub's mergeable and mergeStateStatus. Either can be
// UNKNOWN while GitHub works it out; then it is "".
func mergeState(mergeable, status string) core.Merge {
	if mergeable == "CONFLICTING" || status == "DIRTY" {
		return core.MergeConflicting
	}
	switch status {
	case "CLEAN", "UNSTABLE", "HAS_HOOKS": // UNSTABLE: only optional checks fail
		return core.MergeClean
	case "BEHIND":
		return core.MergeBehind
	case "BLOCKED":
		return core.MergeBlocked
	}
	return ""
}

// DefaultBranch asks gh for the repo's default branch.
func (c Client) DefaultBranch(ctx context.Context, repo core.Repo) (string, error) {
	out, err := c.Exec(ctx, "repo", "view", repo.String(), "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name")
	if err != nil {
		return "", fmt.Errorf("reading the default branch with gh: %w", err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", fmt.Errorf("gh named no default branch for %s", repo)
	}
	return name, nil
}

func toStatus(state string, draft bool) (core.Status, error) {
	switch state {
	case "OPEN":
		if draft {
			return core.StatusDraft, nil
		}
		return core.StatusOpen, nil
	case "MERGED":
		return core.StatusMerged, nil
	case "CLOSED":
		return core.StatusClosed, nil
	}
	return "", fmt.Errorf("unknown pull request state %q from gh", state)
}

// EnvList turns the map into sorted KEY=VALUE pairs so runs are reproducible.
func EnvList(m map[string]string) []string {
	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, k+"="+v)
	}
	slices.Sort(env)
	return env
}
