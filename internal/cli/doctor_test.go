package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// healthyGh answers the commands doctor runs as a logged-in gh would.
func healthyGh(argv []string) ([]byte, error) {
	switch {
	case slices.Contains(argv, "--version"):
		return []byte("gh version 2.102.0 (2026-09-30)\n"), nil
	case slices.Contains(argv, "auth"):
		return nil, nil
	case slices.Contains(argv, "api"):
		return []byte("alice\n"), nil
	}
	return nil, errors.New("unexpected gh call")
}

func doctor(t *testing.T, h *harness) (string, error) {
	t.Helper()
	err := h.run("doctor")
	return h.stdout.String(), err
}

func TestDoctorAllGood(t *testing.T) {
	h := newHarness(t)
	h.gh.respond = healthyGh
	out, err := doctor(t, h)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{"ok    config", "using built-in defaults", "ok    repo", "octo/hello-world", "ok    gh", "gh version 2.102.0", "ok    gh login", "ok    gh user", "alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorMissingGh(t *testing.T) {
	h := newHarness(t)
	h.gh.respond = func([]string) ([]byte, error) { return nil, errors.New(`"gh" not found on PATH`) }
	out, err := doctor(t, h)
	if err == nil {
		t.Fatal("err = nil, want a failure exit")
	}
	for _, want := range []string{"FAIL  gh", "cli.github.com", "skip  gh login", "skip  gh user"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorLoggedOut(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, "repos:\n  octo/hello-world:\n    gh_env: {GH_CONFIG_DIR: /home/alice/.config/gh-personal}\n")
	h.gh.respond = func(argv []string) ([]byte, error) {
		if slices.Contains(argv, "auth") {
			return nil, errors.New("gh: You are not logged into any GitHub hosts")
		}
		return healthyGh(argv)
	}
	out, err := doctor(t, h)
	if err == nil {
		t.Fatal("err = nil, want a failure exit")
	}
	// The hint must log in the same account prledger uses for this repo.
	if !strings.Contains(out, "FAIL  gh login") || !strings.Contains(out, "GH_CONFIG_DIR=/home/alice/.config/gh-personal gh auth login") {
		t.Fatalf("output:\n%s\nwant a login hint with the repo's GH_CONFIG_DIR", out)
	}
}

func TestDoctorBadConfigSkipsTheRest(t *testing.T) {
	h := newHarness(t)
	h.gh.respond = healthyGh
	h.writeConfig(t, "defaults:\n  gh_comand: gh\n")
	out, err := doctor(t, h)
	if err == nil || !strings.Contains(out, "FAIL  config") || !strings.Contains(out, "skip  gh") {
		t.Fatalf("err = %v, output:\n%s", err, out)
	}
}

func TestDoctorWithoutARepoStillChecksGh(t *testing.T) {
	h := newHarness(t)
	h.gh.respond = healthyGh
	h.deps.DetectRepo = func(context.Context) (core.Repo, error) { return core.Repo{}, errors.New("not a git repository") }
	out, err := doctor(t, h)
	if err == nil || !strings.Contains(out, "FAIL  repo") || !strings.Contains(out, "ok    gh user") {
		t.Fatalf("err = %v, output:\n%s", err, out)
	}
}
