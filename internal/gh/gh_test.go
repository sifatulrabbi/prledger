package gh

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// fakeRunner stands in for the gh process.
type fakeRunner struct {
	stdout string
	err    error
	argv   []string
	env    []string
}

func (f *fakeRunner) Run(_ context.Context, argv, env []string) ([]byte, error) {
	f.argv, f.env = argv, env
	return []byte(f.stdout), f.err
}

var query = core.Query{Repo: core.Repo{Owner: "octo", Name: "hello-world"}, Author: "@me", Limit: 500}

func TestListPRsRunsGhPrList(t *testing.T) {
	r := &fakeRunner{stdout: "[]"}
	c := Client{Runner: r, Command: []string{"env", "GH_CONFIG_DIR=/tmp/gh", "gh"}, Env: map[string]string{"B": "2", "A": "1"}}

	if _, err := c.ListPRs(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	wantArgv := []string{
		"env", "GH_CONFIG_DIR=/tmp/gh", "gh",
		"pr", "list", "--repo", "octo/hello-world", "--author", "@me", "--state", "all", "--limit", "500",
		"--json", "number,title,headRefName,baseRefName,state,isDraft,url,createdAt,mergedAt,closedAt,author,labels,assignees",
	}
	if !reflect.DeepEqual(r.argv, wantArgv) {
		t.Errorf("argv =\n%q\nwant\n%q", r.argv, wantArgv)
	}
	if want := []string{"A=1", "B=2"}; !reflect.DeepEqual(r.env, want) {
		t.Errorf("env = %q, want %q", r.env, want)
	}
}

func TestListPRsMapsGhJSON(t *testing.T) {
	r := &fakeRunner{stdout: `[
	  {"number":3,"title":"Add search","headRefName":"feat/search","baseRefName":"fix/login","state":"OPEN","isDraft":true,"url":"https://github.com/octo/hello-world/pull/3","createdAt":"2026-03-01T10:00:00Z","mergedAt":null,"closedAt":null},
	  {"number":2,"title":"Fix login","headRefName":"fix/login","state":"MERGED","isDraft":false,"url":"https://github.com/octo/hello-world/pull/2","createdAt":"2026-02-01T10:00:00Z","mergedAt":"2026-02-03T09:30:00Z","closedAt":"2026-02-03T09:30:00Z"},
	  {"number":1,"title":"Try a thing","headRefName":"spike","state":"CLOSED","isDraft":false,"url":"https://github.com/octo/hello-world/pull/1","createdAt":"2026-01-01T10:00:00Z","mergedAt":null,"closedAt":"2026-01-02T08:00:00Z"},
	  {"number":4,"title":"Ready","headRefName":"ready","state":"OPEN","isDraft":false,"url":"https://github.com/octo/hello-world/pull/4","createdAt":"2026-04-01T10:00:00Z","mergedAt":null,"closedAt":null}
	]`}
	prs, err := Client{Runner: r, Command: []string{"gh"}}.ListPRs(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	ts := func(s string) *time.Time { v, _ := time.Parse(time.RFC3339, s); return &v }
	want := []core.PR{
		{Number: 3, Title: "Add search", Branch: "feat/search", Base: "fix/login", Status: core.StatusDraft, URL: "https://github.com/octo/hello-world/pull/3", CreatedAt: *ts("2026-03-01T10:00:00Z")},
		{Number: 2, Title: "Fix login", Branch: "fix/login", Status: core.StatusMerged, URL: "https://github.com/octo/hello-world/pull/2", CreatedAt: *ts("2026-02-01T10:00:00Z"), MergedAt: ts("2026-02-03T09:30:00Z"), ClosedAt: ts("2026-02-03T09:30:00Z")},
		{Number: 1, Title: "Try a thing", Branch: "spike", Status: core.StatusClosed, URL: "https://github.com/octo/hello-world/pull/1", CreatedAt: *ts("2026-01-01T10:00:00Z"), ClosedAt: ts("2026-01-02T08:00:00Z")},
		{Number: 4, Title: "Ready", Branch: "ready", Status: core.StatusOpen, URL: "https://github.com/octo/hello-world/pull/4", CreatedAt: *ts("2026-04-01T10:00:00Z")},
	}
	if !reflect.DeepEqual(prs, want) {
		t.Fatalf("prs =\n%+v\nwant\n%+v", prs, want)
	}
}

func TestListPRsMapsLabelsAssigneesAndAuthor(t *testing.T) {
	r := &fakeRunner{stdout: `[{"number":5,"state":"OPEN","createdAt":"2026-01-01T10:00:00Z",
	  "author":{"id":"U_1","login":"alice","name":"Alice","is_bot":false},
	  "labels":[{"id":"LA_1","name":"bug","description":"Something is broken","color":"d73a4a"}],
	  "assignees":[{"id":"U_1","login":"alice","name":"Alice"},{"id":"U_2","login":"bob","name":""}]}]`}
	prs, err := Client{Runner: r, Command: []string{"gh"}}.ListPRs(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if want := []core.Label{{Name: "bug", Color: "d73a4a", Description: "Something is broken"}}; !reflect.DeepEqual(prs[0].Labels, want) {
		t.Errorf("Labels = %+v, want %+v", prs[0].Labels, want)
	}
	if want := []string{"alice", "bob"}; !reflect.DeepEqual(prs[0].Assignees, want) {
		t.Errorf("Assignees = %q, want %q", prs[0].Assignees, want)
	}
	if prs[0].Author != "alice" {
		t.Errorf("Author = %q, want alice", prs[0].Author)
	}
}

func TestOpenDetailsAsksForOpenPRsOnly(t *testing.T) {
	r := &fakeRunner{stdout: "[]"}
	if _, err := (Client{Runner: r, Command: []string{"gh"}}).OpenDetails(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"gh", "pr", "list", "--repo", "octo/hello-world", "--author", "@me", "--state", "open", "--limit", "500",
		"--json", "number,reviewDecision,reviewRequests,reviews,statusCheckRollup,mergeable,mergeStateStatus",
	}
	if !reflect.DeepEqual(r.argv, want) {
		t.Fatalf("argv =\n%q\nwant\n%q", r.argv, want)
	}
}

// The shapes below are what gh 2.x prints for these fields.
func TestOpenDetailsMapsReviewsChecksAndMergeState(t *testing.T) {
	r := &fakeRunner{stdout: `[{
	  "number": 7,
	  "reviewDecision": "CHANGES_REQUESTED", "mergeable": "CONFLICTING", "mergeStateStatus": "DIRTY",
	  "reviewRequests": [{"__typename": "User", "login": "carol"}, {"__typename": "Team", "name": "Backend", "slug": "octo/backend"}],
	  "reviews": [
	    {"author": {"login": "dan"}, "state": "CHANGES_REQUESTED", "submittedAt": "2026-03-02T10:00:00Z"},
	    {"author": {"login": "bob"}, "state": "APPROVED", "submittedAt": "2026-03-01T10:00:00Z"},
	    {"author": {"login": "bob"}, "state": "COMMENTED", "submittedAt": "2026-03-05T10:00:00Z"},
	    {"author": {"login": "erin"}, "state": "APPROVED", "submittedAt": "2026-02-28T10:00:00Z"},
	    {"author": {"login": "erin"}, "state": "DISMISSED", "submittedAt": "2026-03-03T10:00:00Z"},
	    {"author": {"login": "github-actions"}, "state": "COMMENTED", "submittedAt": "2026-03-04T10:00:00Z"}
	  ],
	  "statusCheckRollup": [
	    {"__typename": "CheckRun", "name": "test", "status": "COMPLETED", "conclusion": "SUCCESS"},
	    {"__typename": "CheckRun", "name": "lint", "status": "COMPLETED", "conclusion": "SKIPPED"},
	    {"__typename": "CheckRun", "name": "build", "status": "COMPLETED", "conclusion": "FAILURE"},
	    {"__typename": "CheckRun", "name": "e2e", "status": "IN_PROGRESS", "conclusion": ""},
	    {"__typename": "StatusContext", "context": "ci/legacy", "state": "PENDING"},
	    {"__typename": "StatusContext", "context": "deploy", "state": "ERROR"}
	  ]
	}]`}
	got, err := Client{Runner: r, Command: []string{"gh"}}.OpenDetails(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.Details{{
		Number:    7,
		Decision:  core.ReviewChanges,
		Requested: []string{"carol", "octo/backend"},
		// Each reviewer's latest approval or change request, oldest first: a
		// later comment does not undo bob's approval (latestReviews would
		// lose it), erin's approval was dismissed, comments alone say nothing.
		Reviews: []core.Reviewer{
			{Login: "bob", State: core.ReviewerApproved},
			{Login: "dan", State: core.ReviewerChanges},
		},
		Checks: &core.Checks{State: core.CheckFail, Passed: 2, Failed: 2, Pending: 2},
		Merge:  core.MergeConflicting,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details =\n%+v\nwant\n%+v", got, want)
	}
}

func TestMergeState(t *testing.T) {
	tests := []struct {
		mergeable, status string
		want              core.Merge
	}{
		{"MERGEABLE", "CLEAN", core.MergeClean},
		{"MERGEABLE", "UNSTABLE", core.MergeClean}, // only optional checks fail
		{"MERGEABLE", "HAS_HOOKS", core.MergeClean},
		{"MERGEABLE", "BEHIND", core.MergeBehind},
		{"MERGEABLE", "BLOCKED", core.MergeBlocked},
		{"CONFLICTING", "UNKNOWN", core.MergeConflicting},
		{"UNKNOWN", "DIRTY", core.MergeConflicting},
		{"MERGEABLE", "DRAFT", ""},
		{"UNKNOWN", "UNKNOWN", ""},
	}
	for _, tt := range tests {
		if got := mergeState(tt.mergeable, tt.status); got != tt.want {
			t.Errorf("mergeState(%s, %s) = %q, want %q", tt.mergeable, tt.status, got, tt.want)
		}
	}
}

func TestOpenDetailsWrapsGhFailure(t *testing.T) {
	r := &fakeRunner{err: errors.New("HTTP 504: Gateway Timeout")}
	_, err := Client{Runner: r, Command: []string{"gh"}}.OpenDetails(context.Background(), query)
	if err == nil || !strings.Contains(err.Error(), "504") {
		t.Fatalf("err = %v, want gh's message kept", err)
	}
}

func TestListPRsRejectsUnknownState(t *testing.T) {
	r := &fakeRunner{stdout: `[{"number":1,"state":"LOCKED","createdAt":"2026-01-01T10:00:00Z"}]`}
	_, err := Client{Runner: r, Command: []string{"gh"}}.ListPRs(context.Background(), query)
	if err == nil || !strings.Contains(err.Error(), "LOCKED") {
		t.Fatalf("err = %v, want it to name the unknown state", err)
	}
}

func TestListPRsWrapsGhFailure(t *testing.T) {
	r := &fakeRunner{err: errors.New("gh: To get started with GitHub CLI, please run:  gh auth login")}
	_, err := Client{Runner: r, Command: []string{"gh"}}.ListPRs(context.Background(), query)
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("err = %v, want gh's message kept", err)
	}
}

func TestDefaultBranchAsksGhRepoView(t *testing.T) {
	r := &fakeRunner{stdout: "trunk\n"}
	got, err := Client{Runner: r, Command: []string{"gh"}}.DefaultBranch(context.Background(), query.Repo)
	if err != nil || got != "trunk" {
		t.Fatalf("DefaultBranch = %q, %v", got, err)
	}
	if want := []string{"gh", "repo", "view", "octo/hello-world", "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name"}; !reflect.DeepEqual(r.argv, want) {
		t.Fatalf("argv = %q, want %q", r.argv, want)
	}
}

func TestDefaultBranchRejectsEmptyAnswers(t *testing.T) {
	if _, err := (Client{Runner: &fakeRunner{stdout: "\n"}, Command: []string{"gh"}}).DefaultBranch(context.Background(), query.Repo); err == nil {
		t.Fatal("want an error when gh names no default branch")
	}
}

func TestExecRunsAnyGhSubcommand(t *testing.T) {
	r := &fakeRunner{stdout: "alice\n"}
	c := Client{Runner: r, Command: []string{"env", "X=1", "gh"}, Env: map[string]string{"GH_CONFIG_DIR": "/cfg"}}
	out, err := c.Exec(context.Background(), "api", "user", "--jq", ".login")
	if err != nil || string(out) != "alice\n" {
		t.Fatalf("out %q, err %v", out, err)
	}
	if want := []string{"env", "X=1", "gh", "api", "user", "--jq", ".login"}; !reflect.DeepEqual(r.argv, want) {
		t.Fatalf("argv = %q, want %q", r.argv, want)
	}
	if want := []string{"GH_CONFIG_DIR=/cfg"}; !reflect.DeepEqual(r.env, want) {
		t.Fatalf("env = %q, want %q", r.env, want)
	}
}

// Hints print a command the user can paste into a shell, so values with
// spaces or quotes must be quoted.
func TestCommandLineIsPasteable(t *testing.T) {
	c := Client{Command: []string{"gh"}, Env: map[string]string{"GH_CONFIG_DIR": "/Users/a b/gh", "B": "it's"}}
	got := c.CommandLine("auth", "login")
	want := `B='it'\''s' GH_CONFIG_DIR='/Users/a b/gh' gh auth login`
	if got != want {
		t.Fatalf("CommandLine = %s\nwant          %s", got, want)
	}
	if plain := (Client{Command: []string{"gh"}}).CommandLine("auth", "login"); plain != "gh auth login" {
		t.Fatalf("CommandLine = %q, want plain words unquoted", plain)
	}
}

func TestListPRsRejectsEmptyCommand(t *testing.T) {
	_, err := Client{Runner: &fakeRunner{}, Command: nil}.ListPRs(context.Background(), query)
	if err == nil {
		t.Fatal("want an error for an empty gh command")
	}
}
