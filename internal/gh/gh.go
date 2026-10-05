// Package gh is the PRSource adapter backed by the GitHub CLI. prledger never
// talks to GitHub itself: gh does the network calls and owns authentication.
package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
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

const prFields = "number,title,headRefName,state,isDraft,url,createdAt,mergedAt,closedAt"

type ghPR struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	HeadRefName string     `json:"headRefName"`
	State       string     `json:"state"`
	IsDraft     bool       `json:"isDraft"`
	URL         string     `json:"url"`
	CreatedAt   time.Time  `json:"createdAt"`
	MergedAt    *time.Time `json:"mergedAt"`
	ClosedAt    *time.Time `json:"closedAt"`
}

// ListPRs runs `gh pr list` for every state and maps the result.
func (c Client) ListPRs(ctx context.Context, q core.Query) ([]core.PR, error) {
	if len(c.Command) == 0 {
		return nil, errors.New("gh_command is empty")
	}
	argv := append(slices.Clone(c.Command),
		"pr", "list",
		"--repo", q.Repo.String(),
		"--author", q.Author,
		"--state", "all",
		"--limit", strconv.Itoa(q.Limit),
		"--json", prFields,
	)
	out, err := c.Runner.Run(ctx, argv, envList(c.Env))
	if err != nil {
		return nil, fmt.Errorf("listing pull requests with gh: %w", err)
	}

	var raw []ghPR
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("reading gh pr list output: %w", err)
	}
	prs := make([]core.PR, 0, len(raw))
	for _, r := range raw {
		status, err := toStatus(r.State, r.IsDraft)
		if err != nil {
			return nil, fmt.Errorf("PR #%d: %w", r.Number, err)
		}
		prs = append(prs, core.PR{
			Number:    r.Number,
			Title:     r.Title,
			Branch:    r.HeadRefName,
			Status:    status,
			URL:       r.URL,
			CreatedAt: r.CreatedAt,
			MergedAt:  r.MergedAt,
			ClosedAt:  r.ClosedAt,
		})
	}
	return prs, nil
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

// envList turns the map into sorted KEY=VALUE pairs so runs are reproducible.
func envList(m map[string]string) []string {
	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, k+"="+v)
	}
	slices.Sort(env)
	return env
}
