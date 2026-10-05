package gitworktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParsePorcelain(t *testing.T) {
	out := "worktree /repo\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/main\n\n" +
		"worktree /wt/feature-x\nHEAD 2222222222222222222222222222222222222222\nbranch refs/heads/alice/abc-12-api\n\n" +
		"worktree /wt/detached\nHEAD 3333333333333333333333333333333333333333\ndetached\n\n" +
		"worktree /wt/gone\nHEAD 4444444444444444444444444444444444444444\nbranch refs/heads/old\nprunable gitdir file points to non-existent location\n\n"
	want := []entry{
		{Path: "/repo", Branch: "main"},
		{Path: "/wt/feature-x", Branch: "alice/abc-12-api"},
		{Path: "/wt/detached"},
		{Path: "/wt/gone", Branch: "old", Prunable: true},
	}
	if got := parsePorcelain(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePorcelain =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseReflogKeepsTheLatestCheckoutOfEachBranch(t *testing.T) {
	out := "HEAD@{1700000300}\tcheckout: moving from alice/b to alice/c\n" +
		"HEAD@{1700000200}\tcommit: wip\n" +
		"HEAD@{1700000100}\tcheckout: moving from alice/a to alice/b\n" +
		"HEAD@{1700000050}\tcheckout: moving from alice/b to 0123456789abcdef0123456789abcdef01234567\n" +
		"HEAD@{1700000000}\treset: moving to HEAD\n"
	got := parseReflog(out)
	want := map[string]time.Time{
		"alice/c": time.Unix(1700000300, 0),
		"alice/b": time.Unix(1700000300, 0), // left at 300, the latest it was checked out
		"alice/a": time.Unix(1700000100, 0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseReflog = %v, want %v (detached SHAs skipped)", got, want)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestListReadsEveryWorktreeButTheMainCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	git(t, root, "init", "-q", "-b", "main", repo)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, repo, "switch", "-q", "-c", "main-only") // history of the main checkout is ignored
	git(t, repo, "switch", "-q", "main")

	wt := filepath.Join(root, "feature-x")
	git(t, repo, "worktree", "add", "-q", "-b", "alice/abc-61", wt)
	git(t, wt, "switch", "-q", "-c", "alice/abc-62")
	git(t, wt, "switch", "-q", "-c", "alice/abc-63")

	got, err := List(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("worktrees = %+v, want only feature-x", got)
	}
	w := got[0]
	if w.Name != "feature-x" || w.Current != "alice/abc-63" {
		t.Fatalf("worktree = %+v", w)
	}
	var branches []string
	for b := range w.Branches {
		branches = append(branches, b)
	}
	slices.Sort(branches)
	if want := []string{"alice/abc-61", "alice/abc-62", "alice/abc-63"}; !reflect.DeepEqual(branches, want) {
		t.Fatalf("branches = %q, want %q", branches, want)
	}
}

// A worktree whose reflog is gone (expired, or deleted by hand) still counts
// through its current branch.
func TestAWorktreeWithoutAReflogKeepsItsCurrentBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	git(t, root, "init", "-q", "-b", "main", repo)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "fresh")
	git(t, repo, "worktree", "add", "-q", "-b", "alice/abc-70", wt)
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "fresh", "logs")); err != nil {
		t.Fatal(err)
	}
	got, err := List(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Current != "alice/abc-70" {
		t.Fatalf("worktrees = %+v, want fresh with its current branch", got)
	}
}

func TestListStopsWhenCancelled(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := List(ctx, "."); err == nil {
		t.Fatal("want an error from a cancelled context")
	}
}

func TestParseReflogSkipsSHA256Hashes(t *testing.T) {
	out := "HEAD@{1700000000}\tcheckout: moving from alice/a to " + strings.Repeat("ab", 32) + "\n"
	if got := parseReflog(out); len(got) != 1 {
		t.Fatalf("parseReflog = %v, want only alice/a", got)
	}
}

func TestListOutsideARepoIsAnError(t *testing.T) {
	if _, err := List(context.Background(), t.TempDir()); err == nil {
		t.Fatal("want an error outside a git repo")
	}
}
