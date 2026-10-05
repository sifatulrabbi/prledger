// Package config loads prledger's global config file: built-in defaults, a
// `defaults` section, and per-repo sections keyed by owner/name.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// File is the parsed config file.
type File struct {
	Defaults RepoConfig            `yaml:"defaults"`
	Repos    map[string]RepoConfig `yaml:"repos"`
}

// RepoConfig is one section of the file. Unset fields fall back to the
// defaults section, then to the built-in defaults.
type RepoConfig struct {
	GhCommand  Command           `yaml:"gh_command"`
	GhEnv      map[string]string `yaml:"gh_env"`
	Author     string            `yaml:"author"`
	Limit      int               `yaml:"limit"`
	AutoGroups *bool             `yaml:"auto_groups"`
	Groups     []GroupConfig     `yaml:"groups"`
}

// GroupConfig defines one group. A PR joins it if its number is in PRs, or
// its branch or title matches the regular expression in Branch or Title.
type GroupConfig struct {
	Name   string `yaml:"name"`
	PRs    []int  `yaml:"prs"`
	Branch string `yaml:"branch"`
	Title  string `yaml:"title"`
}

// Settings is the resolved configuration for one repo.
type Settings struct {
	Command    []string
	Env        map[string]string
	Author     string
	Limit      int
	AutoGroups bool
	Groups     []core.GroupRule
}

// Command is how to start gh. In YAML it is either a string split on spaces
// ("env GH_CONFIG_DIR=… gh") or a list, for arguments that contain spaces.
type Command []string

// UnmarshalYAML accepts a string or a list of strings.
func (c *Command) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*c = strings.Fields(n.Value)
		if len(*c) == 0 {
			*c = Command{} // set but blank: rejected when resolved
		}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := n.Decode(&list); err != nil {
			return err
		}
		*c = list
		return nil
	}
	return fmt.Errorf("line %d: gh_command must be a string or a list of strings", n.Line)
}

// Load reads the config file at path. A missing file is not an error: it
// yields an empty File, so the built-in defaults apply.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// An empty file decodes to io.EOF; treat it like a missing one.
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("config %s: %w", path, err)
	}
	return f, nil
}

// For resolves the settings for repo. getenv expands $VARS and ~ in
// gh_command and gh_env values.
func (f File) For(repo core.Repo, getenv func(string) string) (Settings, error) {
	s := Settings{Command: []string{"gh"}, Env: map[string]string{}, Author: "@me", Limit: 1000, AutoGroups: true}
	if len(f.Defaults.Groups) > 0 {
		return Settings{}, errors.New("groups belong in a repos.<owner/name> section, not in defaults")
	}
	sections := []RepoConfig{f.Defaults}
	if rc, ok := f.repo(repo); ok {
		sections = append(sections, rc)
		groups, err := compileGroups(rc.Groups)
		if err != nil {
			return Settings{}, fmt.Errorf("%s: %w", repo, err)
		}
		s.Groups = groups
	}
	for _, rc := range sections {
		if rc.GhCommand != nil {
			s.Command = rc.GhCommand
		}
		maps.Copy(s.Env, rc.GhEnv)
		if rc.Author != "" {
			s.Author = rc.Author
		}
		if rc.Limit != 0 {
			s.Limit = rc.Limit
		}
		if rc.AutoGroups != nil {
			s.AutoGroups = *rc.AutoGroups
		}
	}
	if len(s.Command) == 0 {
		return Settings{}, fmt.Errorf("%s: gh_command is empty", repo)
	}
	if s.Limit < 1 {
		return Settings{}, fmt.Errorf("%s: limit must be at least 1, got %d", repo, s.Limit)
	}
	home := getenv("HOME")
	cmd := make([]string, len(s.Command))
	for i, arg := range s.Command {
		cmd[i] = expand(arg, getenv, home)
	}
	s.Command = cmd
	for k, v := range s.Env {
		s.Env[k] = expand(v, getenv, home)
	}
	return s, nil
}

func compileGroups(groups []GroupConfig) ([]core.GroupRule, error) {
	var rules []core.GroupRule
	seen := map[string]bool{}
	for i, g := range groups {
		if strings.TrimSpace(g.Name) == "" {
			return nil, fmt.Errorf("group %d has no name", i+1)
		}
		if seen[g.Name] {
			return nil, fmt.Errorf("group %q is defined twice", g.Name)
		}
		seen[g.Name] = true
		if len(g.PRs) == 0 && g.Branch == "" && g.Title == "" {
			return nil, fmt.Errorf("group %q needs prs, branch or title", g.Name)
		}
		rule := core.GroupRule{Name: g.Name, PRs: g.PRs}
		var err error
		if rule.Branch, err = compile(g.Branch); err != nil {
			return nil, fmt.Errorf("group %q branch: %w", g.Name, err)
		}
		if rule.Title, err = compile(g.Title); err != nil {
			return nil, fmt.Errorf("group %q title: %w", g.Name, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func compile(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

// repo finds the section for r; GitHub owner and repo names are
// case-insensitive, so the lookup is too.
func (f File) repo(r core.Repo) (RepoConfig, bool) {
	for key, rc := range f.Repos {
		if strings.EqualFold(key, r.String()) {
			return rc, true
		}
	}
	return RepoConfig{}, false
}

// expand replaces $VARS and a leading ~/ in s, or in the value of a KEY=VALUE
// argument (as in `env GH_CONFIG_DIR=~/… gh`).
func expand(s string, getenv func(string) string, home string) string {
	s = os.Expand(s, getenv)
	if strings.HasPrefix(s, "~/") {
		return home + s[1:]
	}
	if k, v, ok := strings.Cut(s, "="); ok && strings.HasPrefix(v, "~/") {
		return k + "=" + home + v[1:]
	}
	return s
}

// Path returns the config file to use: $PRLEDGER_CONFIG, else
// $XDG_CONFIG_HOME/prledger/config.yaml, else ~/.config/prledger/config.yaml.
func Path(getenv func(string) string) (string, error) {
	if p := getenv("PRLEDGER_CONFIG"); p != "" {
		return p, nil
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "prledger", "config.yaml"), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", errors.New("cannot find the config file: HOME is not set (set PRLEDGER_CONFIG or pass --config)")
	}
	return filepath.Join(home, ".config", "prledger", "config.yaml"), nil
}

// Starter is the file `prledger config init` writes.
const Starter = `# prledger config. Every key is optional.
# Docs: https://github.com/sifatulrabbi/prledger#configuration

defaults:
  # How to start gh. Runs without a shell, so shell aliases do not work.
  # To use another gh account, point GH_CONFIG_DIR at its config:
  #   gh_command: env GH_CONFIG_DIR=~/.config/gh-personal gh
  gh_command: gh
  author: "@me"
  limit: 1000

# Per-repo settings override the defaults. Keys are owner/name.
repos: {}
#  octo/hello-world:
#    gh_env:
#      GH_CONFIG_DIR: ~/.config/gh-personal
`

// Init writes the starter config to path, creating parent folders. It never
// overwrites an existing file.
func Init(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists; edit it instead", path)
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(Starter); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
