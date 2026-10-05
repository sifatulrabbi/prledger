package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestListUsesTheRepoGhCommandAndEnv(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, `
repos:
  octo/hello-world:
    gh_command: env GH_CONFIG_DIR=~/.config/gh-personal gh
    gh_env: {PAGER: cat}
`)
	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"env", "GH_CONFIG_DIR=/home/alice/.config/gh-personal", "gh", "pr", "list"}; !reflect.DeepEqual(h.gh.argv[:5], want) {
		t.Errorf("argv = %q, want prefix %q", h.gh.argv, want)
	}
	if want := []string{"PAGER=cat"}; !reflect.DeepEqual(h.gh.env, want) {
		t.Errorf("env = %q, want %q", h.gh.env, want)
	}
}

func TestAuthorAndLimitComeFromConfigUnlessFlagsSet(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, "defaults:\n  author: alice\n  limit: 50\n")

	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	if !hasPair(h.gh.argv, "--author", "alice") || !hasPair(h.gh.argv, "--limit", "50") {
		t.Errorf("argv = %q, want config author and limit", h.gh.argv)
	}

	if err := h.run("list", "--json", "--author", "bob", "--limit", "5"); err != nil {
		t.Fatal(err)
	}
	if !hasPair(h.gh.argv, "--author", "bob") || !hasPair(h.gh.argv, "--limit", "5") {
		t.Errorf("argv = %q, want flag author and limit", h.gh.argv)
	}
}

func TestConfigFlagOverridesTheDefaultPath(t *testing.T) {
	h := newHarness(t)
	other := filepath.Join(t.TempDir(), "other.yaml")
	if err := os.WriteFile(other, []byte("defaults: {author: carol}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.run("list", "--json", "--config", other); err != nil {
		t.Fatal(err)
	}
	if !hasPair(h.gh.argv, "--author", "carol") {
		t.Fatalf("argv = %q, want author from --config file", h.gh.argv)
	}
}

func TestListShowsConfiguredGroups(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, `
repos:
  octo/hello-world:
    groups:
      - {name: Login and search, prs: [10, 12]}
`)
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	out := h.stdout.String()
	grouped := strings.Index(out, "Login and search (2)")
	ungrouped := strings.Index(out, "Ungrouped (2)")
	if grouped < 0 || ungrouped < 0 || grouped > ungrouped {
		t.Fatalf("table =\n%s\nwant the configured group before Ungrouped", out)
	}
}

func TestListReportsABadConfig(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, "defaults:\n  gh_comand: gh\n")
	err := h.run("list")
	if err == nil || !strings.Contains(err.Error(), h.env["PRLEDGER_CONFIG"]) {
		t.Fatalf("err = %v, want it to name the config file", err)
	}
}

func TestConfigPathPrintsTheFileInUse(t *testing.T) {
	h := newHarness(t)
	if err := h.run("config", "path"); err != nil {
		t.Fatal(err)
	}
	if got, want := h.stdout.String(), h.env["PRLEDGER_CONFIG"]+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if !strings.Contains(h.stderr.String(), "does not exist yet") {
		t.Fatalf("stderr = %q, want a note that the file is missing", h.stderr.String())
	}
}

func TestConfigInitCreatesTheFileOnce(t *testing.T) {
	h := newHarness(t)
	if err := h.run("config", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.env["PRLEDGER_CONFIG"]); err != nil {
		t.Fatalf("config not created: %v", err)
	}
	if err := h.run("config", "init"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second init err = %v, want already exists", err)
	}
}

func hasPair(argv []string, flag, value string) bool {
	i := slices.Index(argv, flag)
	return i >= 0 && i+1 < len(argv) && argv[i+1] == value
}
