package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func stackedHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "gh-pr-list-stacked.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.gh.stdout = fixture
	return h
}

// The stack shape in the JSON contract: group stack, entries, base.
func TestListJSONDescribesStacks(t *testing.T) {
	h := stackedHarness(t)
	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	golden(t, "list-stacked.golden.json", h.stdout.Bytes())
}

func TestListTableShowsStacksInOrder(t *testing.T) {
	h := stackedHarness(t)
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	golden(t, "list-stacked.golden.txt", h.stdout.Bytes())
}

// A cached snapshot flattens groups; regrouping must rebuild the same stacks.
func TestCachedListRebuildsTheSameStacks(t *testing.T) {
	h := stackedHarness(t)
	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	fresh := h.stdout.String()
	h.stdout.Reset()
	if err := h.run("list", "--json", "--cached"); err != nil {
		t.Fatal(err)
	}
	if got := h.stdout.String(); got != fresh {
		t.Fatalf("cached stacks differ from fresh:\n%s\nvs\n%s", got, fresh)
	}
}
