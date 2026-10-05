// Package gitremote finds which GitHub repo a local checkout belongs to by
// reading its git remote. It runs git, never gh, so the repo is known before
// prledger picks the per-repo gh settings.
package gitremote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// Origin returns the repo that the "origin" remote of the checkout at dir
// points to.
func Origin(ctx context.Context, dir string) (core.Repo, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return core.Repo{}, fmt.Errorf("reading the origin remote: %s (run prledger inside a git repo with an origin remote, or pass --repo owner/name)", msg)
	}
	return ParseRemote(string(out))
}

// ParseRemote extracts owner/name from a git remote URL. It accepts https,
// ssh:// and scp-style (git@host:owner/name) URLs. The host is ignored, so SSH
// host aliases such as personal.github.com work.
func ParseRemote(raw string) (core.Repo, error) {
	s := strings.TrimSpace(raw)
	path, err := remotePath(s)
	if err != nil {
		return core.Repo{}, fmt.Errorf("%q is not a GitHub remote URL: %w", s, err)
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || !validPart(parts[0]) || !validPart(parts[1]) {
		return core.Repo{}, fmt.Errorf("%q is not a GitHub remote URL: want owner/name in the path", s)
	}
	return core.Repo{Owner: parts[0], Name: parts[1]}, nil
}

func remotePath(s string) (string, error) {
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", err
		}
		switch u.Scheme {
		case "https", "http", "ssh", "git":
		default:
			return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
		}
		if u.Host == "" {
			return "", errors.New("missing host")
		}
		return u.Path, nil
	}
	// scp-style: [user@]host:path. A slash before the colon means a local path.
	host, path, ok := strings.Cut(s, ":")
	if !ok || host == "" || strings.Contains(host, "/") {
		return "", errors.New("not a URL")
	}
	return path, nil
}

func validPart(p string) bool {
	return p != "" && !strings.ContainsAny(p, ": \t\r\n")
}
