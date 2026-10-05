package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportHTMLWritesAStandalonePage(t *testing.T) {
	h := newHarness(t)
	out := filepath.Join(t.TempDir(), "mine.html")
	if err := h.run("export", "html", "-o", out); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `"repo":"octo/hello-world"`) || !strings.Contains(string(page), "fix(login): keep the session after refresh") {
		t.Fatal("exported page does not embed the snapshot")
	}
	if got := h.stdout.String(); got != "wrote "+out+"\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestExportHTMLDefaultsToARepoNamedFileHere(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := h.run("export", "html"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prledger-octo-hello-world.html")); err != nil {
		t.Fatalf("default file not written: %v", err)
	}
}

func TestExportHTMLCanUseTheCache(t *testing.T) {
	h := newHarness(t)
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	h.gh.err = errors.New("gh: offline")
	out := filepath.Join(t.TempDir(), "offline.html")
	if err := h.run("export", "html", "--cached", "-o", out); err != nil {
		t.Fatal(err)
	}
}
