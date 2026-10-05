package gitremote

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func gitRepo(t *testing.T, remote string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if remote != "" {
		run("remote", "add", "origin", remote)
	}
	return dir
}

func TestOriginReadsTheOriginRemote(t *testing.T) {
	dir := gitRepo(t, "git@personal.github.com:octo/hello-world.git")
	got, err := Origin(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := (core.Repo{Owner: "octo", Name: "hello-world"}); got != want {
		t.Fatalf("Origin = %+v, want %+v", got, want)
	}
}

func TestOriginWithoutRemoteExplainsTheFix(t *testing.T) {
	dir := gitRepo(t, "")
	_, err := Origin(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "--repo owner/name") {
		t.Fatalf("Origin error = %v, want a hint about --repo", err)
	}
}
