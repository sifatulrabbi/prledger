package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// The fixture's login (#10) and search (#12) PRs came from one worktree.
func featureWorktree() core.Worktree {
	return core.Worktree{
		Name: "alice-login-and-search", Path: "/wt/alice-login-and-search", Current: "alice/abc-12-fuzzy-search",
		Branches: map[string]time.Time{"alice/abc-10-login": time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)},
	}
}

func TestListGroupsByLocalWorktree(t *testing.T) {
	h := newHarness(t)
	h.worktrees = []core.Worktree{featureWorktree()}
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout.String(), "alice-login-and-search (2)") {
		t.Fatalf("table =\n%s\nwant a worktree group with #10 and #12", h.stdout.String())
	}
}

func TestWorktreeGroupsCanBeTurnedOff(t *testing.T) {
	h := newHarness(t)
	h.worktrees = []core.Worktree{featureWorktree()}
	h.writeConfig(t, "repos:\n  octo/hello-world:\n    worktree_groups: false\n")
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.stdout.String(), "alice-login-and-search") {
		t.Fatalf("table =\n%s\nwant no worktree group", h.stdout.String())
	}
}

// Worktrees of the current folder say nothing about another repo.
func TestWorktreesAreIgnoredForAnotherRepo(t *testing.T) {
	h := newHarness(t)
	called := false
	h.deps.Worktrees = func(context.Context) ([]core.Worktree, error) {
		called = true
		return []core.Worktree{featureWorktree()}, nil
	}
	if err := h.run("list", "--repo", "someone/else"); err != nil {
		t.Fatal(err)
	}
	if called || strings.Contains(h.stdout.String(), "alice-login-and-search") {
		t.Fatalf("worktrees used for another repo (called=%v):\n%s", called, h.stdout.String())
	}
}

func TestWorktreesFromTheSameRepoViaRepoFlagAreUsed(t *testing.T) {
	h := newHarness(t)
	h.worktrees = []core.Worktree{featureWorktree()}
	if err := h.run("list", "--repo", "Octo/Hello-World"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout.String(), "alice-login-and-search (2)") {
		t.Fatalf("table =\n%s\nwant the worktree group", h.stdout.String())
	}
}

// Worktrees change while the cache sits on disk; a cached view uses today's.
func TestCachedListUsesTheCurrentWorktrees(t *testing.T) {
	h := newHarness(t)
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	h.worktrees = []core.Worktree{featureWorktree()}
	h.stdout.Reset()
	if err := h.run("list", "--cached"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout.String(), "alice-login-and-search (2)") {
		t.Fatalf("table =\n%s\nwant the worktree that appeared after the fetch", h.stdout.String())
	}
}

func TestUnreadableWorktreesWarnAndCarryOn(t *testing.T) {
	h := newHarness(t)
	h.worktreeErr = errors.New("git worktree list: permission denied")
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stderr.String(), "permission denied") || !strings.Contains(h.stdout.String(), "Ungrouped (4)") {
		t.Fatalf("stderr %q\nstdout %s", h.stderr.String(), h.stdout.String())
	}
}

// Regression: doctor counted this folder's worktrees for `--repo` of another
// repo, unlike list.
func TestDoctorIgnoresWorktreesForAnotherRepo(t *testing.T) {
	h := newHarness(t)
	h.gh.respond = healthyGh
	h.worktrees = []core.Worktree{featureWorktree()}
	h.deps.Stdout = &h.stdout
	if err := h.run("doctor", "--repo", "someone/else"); err != nil {
		t.Fatalf("err = %v\n%s", err, h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "worktrees not used: this folder is not a checkout of someone/else") {
		t.Fatalf("output:\n%s", h.stdout.String())
	}
}

// The snapshot names the worktree field; the page reads it.
func TestListJSONCarriesTheWorktreePath(t *testing.T) {
	h := newHarness(t)
	h.worktrees = []core.Worktree{featureWorktree()}
	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	golden(t, "list-worktree.golden.json", h.stdout.Bytes())
}

// Paths under the home folder are shown as ~/…, and an exported page, which
// is meant to be shared, carries no local paths at all.
func TestWorktreePathsAreShortenedAndLeftOutOfExports(t *testing.T) {
	h := newHarness(t)
	w := featureWorktree()
	w.Path = "/home/alice/.herdr/worktrees/x"
	h.worktrees = []core.Worktree{w}
	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	if out := h.stdout.String(); !strings.Contains(out, `"worktree": "~/.herdr/worktrees/x"`) {
		t.Fatalf("list --json =\n%s\nwant the path shortened to ~/", out)
	}
	out := filepath.Join(t.TempDir(), "page.html")
	if err := h.run("export", "html", "-o", out); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(out)
	if strings.Contains(string(page), ".herdr") || !strings.Contains(string(page), "alice-login-and-search") {
		t.Fatal("export should keep the worktree group but not its path")
	}
}

// Under serve every refresh rebuilds the ledger; a broken worktree setup
// must not print the same warning each time.
func TestTheWorktreeWarningPrintsOnce(t *testing.T) {
	h := newHarness(t)
	h.worktreeErr = errors.New("boom")
	tgt := target{repo: core.Repo{Owner: "octo", Name: "hello-world"}, local: true, warned: new(sync.Once)}
	tgt.settings.WorktreeGroups = true
	tgt.worktrees(context.Background(), h.deps)
	tgt.worktrees(context.Background(), h.deps)
	if n := strings.Count(h.stderr.String(), "boom"); n != 1 {
		t.Fatalf("warning printed %d times, want once:\n%s", n, h.stderr.String())
	}
}

func TestDoctorNotesUnreadableWorktreesWithoutFailing(t *testing.T) {
	h := newHarness(t)
	h.gh.respond = healthyGh
	h.worktreeErr = errors.New("git worktree list: boom")
	out, err := doctor(t, h)
	if err != nil || !strings.Contains(out, "note  worktrees not grouping by worktree: git worktree list: boom") {
		t.Fatalf("err = %v, output:\n%s", err, out)
	}
}

func TestDoctorReportsWorktrees(t *testing.T) {
	h := newHarness(t)
	h.gh.respond = healthyGh
	h.worktrees = []core.Worktree{featureWorktree(), {Name: "other", Path: "/wt/other"}}
	out, err := doctor(t, h)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok    worktrees 2 found") {
		t.Fatalf("output:\n%s\nwant a worktrees line", out)
	}
}
