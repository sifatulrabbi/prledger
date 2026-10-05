// Package gitworktree reads a repo's local git worktrees and the branches each
// has had checked out, from `git worktree list` and each worktree's HEAD
// reflog.
package gitworktree

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// List returns the worktrees of the repo at dir, except the main checkout:
// that is the shared base, not one stream of work.
func List(ctx context.Context, dir string) ([]core.Worktree, error) {
	out, err := run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	entries := parsePorcelain(out)
	var wts []core.Worktree
	for i, e := range entries {
		if i == 0 || e.Prunable {
			continue
		}
		w := core.Worktree{Name: filepath.Base(e.Path), Path: e.Path, Current: e.Branch, Branches: map[string]time.Time{}}
		// A worktree with no reflog still counts through its current branch.
		log, err := run(ctx, e.Path, "reflog", "show", "--date=unix", "--format=%gd%x09%gs", "HEAD")
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			w.Branches = parseReflog(log)
		}
		wts = append(wts, w)
	}
	return wts, nil
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

type entry struct {
	Path     string
	Branch   string // "" when detached
	Prunable bool
}

func parsePorcelain(out string) []entry {
	var entries []entry
	for block := range strings.SplitSeq(strings.TrimSpace(out), "\n\n") {
		var e entry
		for line := range strings.SplitSeq(block, "\n") {
			key, value, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				e.Path = value
			case "branch":
				e.Branch = strings.TrimPrefix(value, "refs/heads/")
			case "prunable":
				e.Prunable = true
			}
		}
		if e.Path != "" {
			entries = append(entries, e)
		}
	}
	return entries
}

var (
	reflogLine = regexp.MustCompile(`^HEAD@\{(\d+)\}\tcheckout: moving from (\S+) to (\S+)$`)
	commitSHA  = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`) // SHA-1 or SHA-256 repos
)

// parseReflog returns each branch the worktree checked out, or left by
// checking out another, with the latest time it was there.
func parseReflog(out string) map[string]time.Time {
	branches := map[string]time.Time{}
	for line := range strings.SplitSeq(out, "\n") {
		m := reflogLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		secs, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		at := time.Unix(secs, 0)
		for _, b := range m[2:] {
			if commitSHA.MatchString(b) {
				continue
			}
			if old, seen := branches[b]; !seen || at.After(old) {
				branches[b] = at
			}
		}
	}
	return branches
}
