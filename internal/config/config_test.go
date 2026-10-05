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
