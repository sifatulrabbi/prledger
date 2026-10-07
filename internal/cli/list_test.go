package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fakeGh stands in for the gh process. Without respond it answers
// `gh repo view` with "main", the open-PR details call with details (or []),
// the GraphQL discussions call with discussions (or none), and every other
// run with stdout/err; respond, when set, answers every call. It records the
// argv and env of the last call that is none of those three (the pr list
// call tests inspect). The details and discussions calls run concurrently.
type fakeGh struct {
	stdout      []byte
	details     []byte
	discussions []byte
	err         error
	argv        []string
	env         []string
	// respond, when set, answers instead of stdout/err, e.g. per subcommand.
	respond func(argv []string) ([]byte, error)
}

func isDiscussions(argv []string) bool { return slices.Contains(argv, "graphql") }

func (f *fakeGh) Run(_ context.Context, argv, env []string) ([]byte, error) {
	repoView := slices.Contains(argv, "repo") && slices.Contains(argv, "view")
	details := hasPair(argv, "--state", "open")
	talk := isDiscussions(argv)
	if !repoView && !details && !talk {
		f.argv, f.env = argv, env // the pr list call the tests inspect
	}
	switch {
	case f.respond != nil:
		return f.respond(argv)
	case f.err != nil:
		return nil, f.err
	case repoView:
		return []byte("main\n"), nil // gh repo view … --jq .defaultBranchRef.name
	case details && f.details != nil:
		return f.details, nil
	case details:
		return []byte("[]"), nil
	case talk && f.discussions != nil:
		return f.discussions, nil
	case talk:
		return []byte(`{"data":{"search":{"nodes":[]}}}`), nil
	}
	return f.stdout, nil
}

type harness struct {
	gh     *fakeGh
	stdout bytes.Buffer
	stderr bytes.Buffer
	env    map[string]string
	deps   Deps
}

// writeConfig writes body to the harness's config file.
func (h *harness) writeConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(h.env["PRLEDGER_CONFIG"], []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", "gh-pr-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	details, err := os.ReadFile(filepath.Join("testdata", "gh-pr-details.json"))
	if err != nil {
		t.Fatal(err)
	}
	discussions, err := os.ReadFile(filepath.Join("testdata", "gh-pr-discussions.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{gh: &fakeGh{stdout: fixture, details: details, discussions: discussions}}
	h.env = map[string]string{
		"HOME":            "/home/alice",
		"PRLEDGER_CONFIG": filepath.Join(t.TempDir(), "config.yaml"), // never the real one
		"XDG_CACHE_HOME":  t.TempDir(),
	}
	h.deps = Deps{
		Getenv:     func(k string) string { return h.env[k] },
		Stdout:     &h.stdout,
		Stderr:     &h.stderr,
		Version:    "test",
		Now:        func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) },
		Runner:     h.gh,
		DetectRepo: func(context.Context) (core.Repo, error) { return core.Repo{Owner: "octo", Name: "hello-world"}, nil },
	}
	return h
}

func (h *harness) run(args ...string) error {
	root := NewRoot(h.deps)
	root.SetArgs(args)
	return root.Execute()
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test ./internal/cli -update` to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from output:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// The JSON printed by `list --json` is the contract every frontend reads.
func TestListJSONMatchesTheContract(t *testing.T) {
	h := newHarness(t)
	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	golden(t, "list.golden.json", h.stdout.Bytes())
}

func TestListPrintsATable(t *testing.T) {
	h := newHarness(t)
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	golden(t, "list.golden.txt", h.stdout.Bytes())
}

func TestListRepoFlagSkipsDetection(t *testing.T) {
	h := newHarness(t)
	h.deps.DetectRepo = func(context.Context) (core.Repo, error) { return core.Repo{}, errors.New("not a git repo") }
	if err := h.run("list", "--json", "--repo", "someone/else"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(h.gh.argv, " "), "--repo someone/else") {
		t.Fatalf("gh argv = %q, want --repo someone/else", h.gh.argv)
	}
}

func TestListRejectsMalformedRepoFlag(t *testing.T) {
	h := newHarness(t)
	err := h.run("list", "--repo", "just-a-name")
	if err == nil || !strings.Contains(err.Error(), "owner/name") {
		t.Fatalf("err = %v, want a hint about owner/name", err)
	}
}

func TestListReportsDetectionFailure(t *testing.T) {
	h := newHarness(t)
	h.deps.DetectRepo = func(context.Context) (core.Repo, error) { return core.Repo{}, errors.New("no origin remote") }
	if err := h.run("list"); err == nil || !strings.Contains(err.Error(), "no origin remote") {
		t.Fatalf("err = %v", err)
	}
}

// GitHub times out on the slow fields for busy repos; the list must survive.
func TestListWarnsWhenDetailsFail(t *testing.T) {
	h := newHarness(t)
	list := h.gh.stdout
	h.gh.respond = func(argv []string) ([]byte, error) {
		switch {
		case hasPair(argv, "--state", "open"):
			return nil, errors.New("HTTP 504: Gateway Timeout")
		case isDiscussions(argv):
			return nil, errors.New("GraphQL: Something went wrong")
		case slices.Contains(argv, "view"):
			return []byte("main\n"), nil
		}
		return list, nil
	}
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"warning: Reviews, CI and merge state are not shown", "504", "warning: Review comments are not shown", "Something went wrong"} {
		if !strings.Contains(h.stdout.String(), "#12") || !strings.Contains(h.stderr.String(), want) {
			t.Fatalf("stdout:\n%s\nstderr:\n%s\nwant the PRs and %q", h.stdout.String(), h.stderr.String(), want)
		}
	}
}

func TestListReportsGhFailure(t *testing.T) {
	h := newHarness(t)
	h.gh.err = errors.New("gh: not logged in")
	if err := h.run("list"); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}

func TestSignalsNameWhoAskedForChangesAndWhoseThreadsAreOpen(t *testing.T) {
	pr := core.PR{
		Review: core.ReviewChanges,
		Reviewers: []core.Reviewer{
			{Login: "bob", State: core.ReviewerApproved},
			{Login: "dan", State: core.ReviewerChanges},
		},
		Commenters: []core.Commenter{
			{Login: "carol", Comments: 3, Unresolved: 2},
			{Login: "a", Comments: 1}, {Login: "b", Comments: 1}, {Login: "c", Comments: 1}, {Login: "d", Comments: 1},
		},
	}
	want := "changes requested by dan · 2 unresolved (carol) · comments: a, b, c +1"
	if got := signals(pr); got != want {
		t.Fatalf("signals = %q\nwant      %q", got, want)
	}
}
