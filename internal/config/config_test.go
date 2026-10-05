package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
)

var repo = core.Repo{Owner: "octo", Name: "hello-world"}

// env is a fixed environment for expanding $VARS.
func env(key string) string {
	return map[string]string{"HOME": "/home/alice", "XDG_CONFIG_HOME": ""}[key]
}

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func settingsFor(t *testing.T, body string) Settings {
	t.Helper()
	f, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.For(repo, env)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMissingFileGivesBuiltInDefaults(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := f.For(repo, env)
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{Command: []string{"gh"}, Env: map[string]string{}, Author: "@me", Limit: 1000, AutoGroups: true}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("settings = %+v, want %+v", s, want)
	}
}

func TestEmptyFileGivesBuiltInDefaults(t *testing.T) {
	if s := settingsFor(t, "# nothing yet\n"); s.Author != "@me" || s.Limit != 1000 {
		t.Fatalf("settings = %+v, want built-in defaults", s)
	}
}

func TestRepoSectionOverridesDefaults(t *testing.T) {
	s := settingsFor(t, `
defaults:
  author: alice
  limit: 200
  gh_env: {GH_HOST: github.com, PAGER: cat}
repos:
  Octo/Hello-World:   # GitHub names are case-insensitive
    gh_command: env GH_CONFIG_DIR=/cfg/gh-personal gh
    gh_env: {PAGER: less}
    limit: 50
`)
	want := Settings{
		Command:    []string{"env", "GH_CONFIG_DIR=/cfg/gh-personal", "gh"},
		Env:        map[string]string{"GH_HOST": "github.com", "PAGER": "less"},
		Author:     "alice",
		Limit:      50,
		AutoGroups: true,
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("settings = %+v, want %+v", s, want)
	}
}

func TestOtherReposKeepDefaults(t *testing.T) {
	s := settingsFor(t, `
repos:
  someone/else:
    author: bob
`)
	if s.Author != "@me" {
		t.Fatalf("author = %q, want the default", s.Author)
	}
}

// The list form exists for arguments that contain spaces.
func TestGhCommandListForm(t *testing.T) {
	s := settingsFor(t, `
defaults:
  gh_command: ["/opt/my tools/gh"]
`)
	if want := []string{"/opt/my tools/gh"}; !reflect.DeepEqual(s.Command, want) {
		t.Fatalf("command = %q, want %q", s.Command, want)
	}
}

func TestHomeAndVariablesAreExpanded(t *testing.T) {
	s := settingsFor(t, `
defaults:
  gh_command: env GH_CONFIG_DIR=~/.config/gh-personal ~/bin/gh
  gh_env: {GH_CONFIG_DIR: $HOME/.config/gh-personal, CACHE: ~/cache}
`)
	if want := []string{"env", "GH_CONFIG_DIR=/home/alice/.config/gh-personal", "/home/alice/bin/gh"}; !reflect.DeepEqual(s.Command, want) {
		t.Errorf("command = %q, want %q", s.Command, want)
	}
	if want := map[string]string{"GH_CONFIG_DIR": "/home/alice/.config/gh-personal", "CACHE": "/home/alice/cache"}; !reflect.DeepEqual(s.Env, want) {
		t.Errorf("env = %q, want %q", s.Env, want)
	}
}

func TestUnknownKeyNamesFileAndLine(t *testing.T) {
	path := write(t, "defaults:\n  author: alice\n  gh_comand: gh\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("err = %v, want the file path and line 3", err)
	}
}

func TestBadYAMLNamesFile(t *testing.T) {
	path := write(t, "defaults: [unclosed\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want the file path", err)
	}
}

func TestNegativeLimitIsRejected(t *testing.T) {
	f, err := Load(write(t, "repos:\n  octo/hello-world:\n    limit: -5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.For(repo, env); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want a limit error", err)
	}
}

func TestEmptyGhCommandIsRejected(t *testing.T) {
	f, err := Load(write(t, "defaults:\n  gh_command: \"  \"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.For(repo, env); err == nil || !strings.Contains(err.Error(), "gh_command") {
		t.Fatalf("err = %v, want a gh_command error", err)
	}
}

func TestGroupsResolveToRules(t *testing.T) {
	s := settingsFor(t, `
repos:
  octo/hello-world:
    auto_groups: false
    groups:
      - name: Teams notifications
        prs: [41, 42]
        branch: "teams-"
      - name: Docs
        title: "(?i)^docs"
`)
	if s.AutoGroups {
		t.Error("auto_groups: false was ignored")
	}
	if len(s.Groups) != 2 {
		t.Fatalf("groups = %+v", s.Groups)
	}
	teams, docs := s.Groups[0], s.Groups[1]
	if teams.Name != "Teams notifications" || !reflect.DeepEqual(teams.PRs, []int{41, 42}) ||
		teams.Branch == nil || !teams.Branch.MatchString("alice/teams-dm") || teams.Title != nil {
		t.Errorf("teams rule = %+v", teams)
	}
	if docs.Name != "Docs" || docs.Branch != nil || docs.Title == nil || !docs.Title.MatchString("Docs: readme") {
		t.Errorf("docs rule = %+v", docs)
	}
}

func TestBadGroupsAreReportedByName(t *testing.T) {
	cases := map[string]string{
		"invalid regex":    "    groups:\n      - {name: Broken, branch: \"(\"}\n",
		"missing name":     "    groups:\n      - {prs: [1]}\n",
		"nothing to match": "    groups:\n      - {name: Empty}\n",
		"duplicate name":   "    groups:\n      - {name: Twice, prs: [1]}\n      - {name: Twice, prs: [2]}\n",
	}
	wantInErr := map[string]string{
		"invalid regex":    `"Broken"`,
		"missing name":     "name",
		"nothing to match": `"Empty"`,
		"duplicate name":   `"Twice"`,
	}
	for name, groups := range cases {
		t.Run(name, func(t *testing.T) {
			if err := configErr(t, "repos:\n  octo/hello-world:\n"+groups); err == nil || !strings.Contains(err.Error(), wantInErr[name]) {
				t.Fatalf("err = %v, want it to mention %s", err, wantInErr[name])
			}
		})
	}
}

func TestTicketPrefixes(t *testing.T) {
	s := settingsFor(t, "defaults:\n  ticket_prefixes: [SEQ]\nrepos:\n  octo/hello-world:\n    ticket_prefixes: [PRO, seq]\n")
	if want := []string{"PRO", "seq"}; !reflect.DeepEqual(s.TicketPrefixes, want) {
		t.Fatalf("prefixes = %q, want %q", s.TicketPrefixes, want)
	}
	if err := configErr(t, "defaults:\n  ticket_prefixes: [\"SE Q\"]\n"); err == nil || !strings.Contains(err.Error(), "ticket_prefixes") {
		t.Fatalf("err = %v, want a ticket_prefixes error", err)
	}
}

// Regression: only the current repo's section was checked, so a broken
// regex elsewhere went unnoticed until you ran prledger in that repo.
func TestLoadChecksEveryRepoSection(t *testing.T) {
	path := write(t, "repos:\n  someone/else:\n    groups:\n      - {name: Broken, title: \"[\"}\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "someone/else") || !strings.Contains(err.Error(), `"Broken"`) {
		t.Fatalf("err = %v, want the repo and group named", err)
	}
}

// Regression: keys differing only in case made the section chosen depend on
// map iteration order.
func TestRepoKeysDifferingOnlyInCaseAreRejected(t *testing.T) {
	_, err := Load(write(t, "repos:\n  octo/hello-world: {author: a}\n  Octo/Hello-World: {author: b}\n"))
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("err = %v, want a duplicate repo error", err)
	}
}

// worktree_groups was removed when stacks replaced worktree grouping; a
// config that still has it gets told what to do instead of a generic
// "field not found".
func TestTheRemovedWorktreeGroupsKeyExplainsItself(t *testing.T) {
	err := configErr(t, "repos:\n  octo/hello-world:\n    worktree_groups: false\n")
	if err == nil || !strings.Contains(err.Error(), "worktree_groups was removed") || !strings.Contains(err.Error(), "octo/hello-world") {
		t.Fatalf("err = %v, want a removal note naming the section", err)
	}
}

func TestGroupsInDefaultsAreRejected(t *testing.T) {
	if err := configErr(t, "defaults:\n  groups:\n    - {name: X, prs: [1]}\n"); err == nil || !strings.Contains(err.Error(), "repos.") {
		t.Fatalf("err = %v, want a hint to move groups under repos", err)
	}
}

// configErr loads body and resolves it for repo, returning the first error.
func configErr(t *testing.T, body string) error {
	t.Helper()
	f, err := Load(write(t, body))
	if err != nil {
		return err
	}
	_, err = f.For(repo, env)
	return err
}

func TestPathPrecedence(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"PRLEDGER_CONFIG wins", map[string]string{"PRLEDGER_CONFIG": "/x/p.yaml", "XDG_CONFIG_HOME": "/xdg", "HOME": "/home/alice"}, "/x/p.yaml"},
		{"then XDG_CONFIG_HOME", map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/home/alice"}, "/xdg/prledger/config.yaml"},
		{"then ~/.config", map[string]string{"HOME": "/home/alice"}, "/home/alice/.config/prledger/config.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Path(func(k string) string { return c.vars[k] })
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("Path = %q, want %q", got, c.want)
			}
		})
	}
}

func TestInitWritesAStarterThatLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("starter config does not load: %v", err)
	}
	if _, err := f.For(repo, env); err != nil {
		t.Fatalf("starter config does not resolve: %v", err)
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	path := write(t, "defaults: {author: alice}\n")
	if err := Init(path); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want already exists", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "defaults: {author: alice}\n" {
		t.Fatalf("file was changed: %q", b)
	}
}
