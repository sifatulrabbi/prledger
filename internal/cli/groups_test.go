package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

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

// Suggesting groups needs titles and branches only: it must not pay for the
// slow details call, nor replace the cache with a snapshot that lacks them.
func TestGroupsSuggestSkipsDetailsAndKeepsTheCache(t *testing.T) {
	h := linkedHarness(t)
	h.gh.details = []byte(`[{"number":26,"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]}]`)
	if err := h.run("list"); err != nil { // fills the cache, details included
		t.Fatal(err)
	}
	list := h.gh.stdout
	h.gh.respond = func(argv []string) ([]byte, error) {
		switch {
		case hasPair(argv, "--state", "open"):
			t.Errorf("groups suggest asked gh for details: %q", argv)
			return []byte("[]"), nil
		case isDiscussions(argv):
			t.Errorf("groups suggest asked gh for comments: %q", argv)
			return nil, errors.New("not expected")
		case slices.Contains(argv, "view"):
			return []byte("main\n"), nil
		}
		return list, nil
	}
	if err := h.run("groups", "suggest"); err != nil {
		t.Fatal(err)
	}
	h.stdout.Reset()
	if err := h.run("list", "--cached", "--json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout.String(), `"checks"`) {
		t.Fatalf("cache lost its details after groups suggest:\n%s", h.stdout.String())
	}
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

// Regression: with groups already configured, the suggestion repeated the
// groups: key, and pasting it made the config fail to load.
func TestSuggestionAppendsToExistingGroups(t *testing.T) {
	h := linkedHarness(t)
	existing := "repos:\n  octo/hello-world:\n    auto_groups: false\n    groups:\n      - {name: Readme, prs: [21]}\n"
	h.writeConfig(t, existing)
	if err := h.run("groups", "suggest"); err != nil {
		t.Fatal(err)
	}
	suggestion := h.stdout.String()
	if strings.Contains(suggestion, "groups:") && !strings.HasPrefix(strings.TrimSpace(suggestion), "#") {
		t.Fatalf("suggestion repeats the groups key:\n%s", suggestion)
	}
	var indented []string
	for line := range strings.SplitSeq(suggestion, "\n") {
		indented = append(indented, "      "+line)
	}
	h.writeConfig(t, existing+strings.Join(indented, "\n"))
	h.stdout.Reset()
	if err := h.run("list", "--json"); err != nil {
		t.Fatalf("config with the appended suggestion does not load: %v\n%s", err, suggestion)
	}
	var snap core.Snapshot
	json.Unmarshal(h.stdout.Bytes(), &snap)
	if len(snap.Groups) != 3 || snap.Groups[len(snap.Groups)-1].Name == core.UngroupedName {
		t.Fatalf("groups = %+v, want Readme plus the two suggested ones", snap.Groups)
	}
}

// Regression: two families whose oldest PRs share a title got the same
// suggested name, which the config then rejected as defined twice.
func TestSuggestedNamesAreUnique(t *testing.T) {
	h := newHarness(t)
	h.gh.stdout = []byte(`[
	  {"number":4,"title":"chore: bump deps","headRefName":"bob/deps-split/b","state":"MERGED","isDraft":false,"url":"https://github.com/octo/hello-world/pull/4","createdAt":"2026-09-04T10:00:00Z","mergedAt":"2026-09-04T11:00:00Z","closedAt":"2026-09-04T11:00:00Z"},
	  {"number":3,"title":"chore: bump deps","headRefName":"bob/deps-split/a","state":"MERGED","isDraft":false,"url":"https://github.com/octo/hello-world/pull/3","createdAt":"2026-09-03T10:00:00Z","mergedAt":"2026-09-03T11:00:00Z","closedAt":"2026-09-03T11:00:00Z"},
	  {"number":2,"title":"chore: bump deps","headRefName":"alice/deps-split/b","state":"MERGED","isDraft":false,"url":"https://github.com/octo/hello-world/pull/2","createdAt":"2026-09-02T10:00:00Z","mergedAt":"2026-09-02T11:00:00Z","closedAt":"2026-09-02T11:00:00Z"},
	  {"number":1,"title":"chore: bump deps","headRefName":"alice/deps-split/a","state":"MERGED","isDraft":false,"url":"https://github.com/octo/hello-world/pull/1","createdAt":"2026-09-01T10:00:00Z","mergedAt":"2026-09-01T11:00:00Z","closedAt":"2026-09-01T11:00:00Z"}
	]`)
	h.writeConfig(t, "repos:\n  octo/hello-world:\n    groups:\n      - {name: \"chore: bump deps (2)\", prs: [99]}\n")
	if err := h.run("groups", "suggest"); err != nil {
		t.Fatal(err)
	}
	var items []struct{ Name string }
	if err := yaml.Unmarshal(h.stdout.Bytes(), &items); err != nil {
		t.Fatalf("not a YAML list: %v\n%s", err, h.stdout.String())
	}
	if len(items) != 2 || items[0].Name == items[1].Name || items[0].Name == "chore: bump deps (2)" || items[1].Name == "chore: bump deps (2)" {
		t.Fatalf("names = %+v, want two distinct names that avoid the configured one", items)
	}
}

// Regression: a title with a line break ended the comment it was printed in,
// so the rest of the title became live YAML in the pasted config.
func TestUnlinkedTitlesCannotInjectYAML(t *testing.T) {
	for _, brk := range []string{"\n", "\r", " ", "\u0085"} {
		h := newHarness(t)
		h.gh.stdout = []byte(`[{"number":1,"title":"a` + jsonEscape(brk) + `evil: 1","headRefName":"x","state":"OPEN","isDraft":false,"url":"https://github.com/octo/hello-world/pull/1","createdAt":"2026-09-01T10:00:00Z","mergedAt":null,"closedAt":null}]`)
		if err := h.run("groups", "suggest"); err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err := yaml.Unmarshal(h.stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("break %q: output is not YAML: %v", brk, err)
		}
		if _, injected := parsed["evil"]; injected {
			t.Fatalf("break %q: title injected a key:\n%s", brk, h.stdout.String())
		}
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
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
