package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fakeGh stands in for the gh process: it answers every run with stdout/err
// and records the argv and env it was given.
type fakeGh struct {
	stdout []byte
	err    error
	argv   []string
	env    []string
}

func (f *fakeGh) Run(_ context.Context, argv, env []string) ([]byte, error) {
	f.argv, f.env = argv, env
	return f.stdout, f.err
}

type harness struct {
	gh     *fakeGh
	stdout bytes.Buffer
	stderr bytes.Buffer
	deps   Deps
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", "gh-pr-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{gh: &fakeGh{stdout: fixture}}
	h.deps = Deps{
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

func TestListReportsGhFailure(t *testing.T) {
	h := newHarness(t)
	h.gh.err = errors.New("gh: not logged in")
	if err := h.run("list"); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}
