package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func linkedHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "gh-pr-list-linked.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.gh.stdout = fixture
	return h
}

func TestGroupsSuggestPrintsPasteableYAML(t *testing.T) {
	h := linkedHarness(t)
	if err := h.run("groups", "suggest"); err != nil {
		t.Fatal(err)
	}
	golden(t, "groups-suggest.golden.yaml", h.stdout.Bytes())
}

// Pasting the suggestion into the config reproduces the suggested groups.
func TestGroupsSuggestionRoundTrips(t *testing.T) {
	h := linkedHarness(t)
	if err := h.run("groups", "suggest"); err != nil {
		t.Fatal(err)
	}
	suggestion := h.stdout.String()

	var indented []string
	for line := range strings.SplitSeq(suggestion, "\n") {
		indented = append(indented, "    "+line)
	}
	h.writeConfig(t, "repos:\n  octo/hello-world:\n    auto_groups: false\n"+strings.Join(indented, "\n"))
	h.stdout.Reset()
	if err := h.run("list", "--json"); err != nil {
		t.Fatalf("pasted suggestion does not load: %v\n%s", err, suggestion)
	}
	var snap core.Snapshot
	if err := json.Unmarshal(h.stdout.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	got := map[string][]int{}
	for _, g := range snap.Groups {
		for _, p := range g.PRs {
			got[g.Name] = append(got[g.Name], p.Number)
		}
	}
	want := map[string][]int{
		"ABC-21 · feat(api): search endpoint": {26, 25},
		"feat: cache layer":                   {24, 23, 22},
		core.UngroupedName:                    {21},
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %v, want %v", got, want)
	}
	for name, nums := range want {
		if !equalNums(got[name], nums) {
			t.Fatalf("groups = %v, want %v", got, want)
		}
	}
}

func TestGroupsSuggestLeavesConfiguredPRsOut(t *testing.T) {
	h := linkedHarness(t)
	h.writeConfig(t, "repos:\n  octo/hello-world:\n    groups:\n      - {name: Cache, branch: cache}\n")
	if err := h.run("groups", "suggest"); err != nil {
		t.Fatal(err)
	}
	if out := h.stdout.String(); strings.Contains(out, "cache layer") {
		t.Fatalf("suggestion repeats configured PRs:\n%s", out)
	}
}

func equalNums(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
