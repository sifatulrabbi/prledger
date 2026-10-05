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
		"--json", "number,title,headRefName,state,isDraft,url,createdAt,mergedAt,closedAt",
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
	  {"number":3,"title":"Add search","headRefName":"feat/search","state":"OPEN","isDraft":true,"url":"https://github.com/octo/hello-world/pull/3","createdAt":"2026-03-01T10:00:00Z","mergedAt":null,"closedAt":null},
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
		{Number: 3, Title: "Add search", Branch: "feat/search", Status: core.StatusDraft, URL: "https://github.com/octo/hello-world/pull/3", CreatedAt: *ts("2026-03-01T10:00:00Z")},
		{Number: 2, Title: "Fix login", Branch: "fix/login", Status: core.StatusMerged, URL: "https://github.com/octo/hello-world/pull/2", CreatedAt: *ts("2026-02-01T10:00:00Z"), MergedAt: ts("2026-02-03T09:30:00Z"), ClosedAt: ts("2026-02-03T09:30:00Z")},
		{Number: 1, Title: "Try a thing", Branch: "spike", Status: core.StatusClosed, URL: "https://github.com/octo/hello-world/pull/1", CreatedAt: *ts("2026-01-01T10:00:00Z"), ClosedAt: ts("2026-01-02T08:00:00Z")},
		{Number: 4, Title: "Ready", Branch: "ready", Status: core.StatusOpen, URL: "https://github.com/octo/hello-world/pull/4", CreatedAt: *ts("2026-04-01T10:00:00Z")},
	}
	if !reflect.DeepEqual(prs, want) {
		t.Fatalf("prs =\n%+v\nwant\n%+v", prs, want)
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
