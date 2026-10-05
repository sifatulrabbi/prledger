package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

var repo = core.Repo{Owner: "octo", Name: "hello-world"}

func sample() core.Snapshot {
	return core.Snapshot{
		Schema: core.SchemaVersion, Repo: "octo/hello-world", Author: "@me",
		FetchedAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
		Groups:    []core.Group{{Name: "Ungrouped", PRs: []core.PR{{Number: 7, Title: "Add search", Status: core.StatusOpen}}}},
	}
}

func TestSaveThenLoad(t *testing.T) {
	f := File{Path: filepath.Join(t.TempDir(), "a", "b.json")}
	if err := f.Save(sample()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := f.Load()
	if err != nil || !ok {
		t.Fatalf("Load = ok %v, err %v", ok, err)
	}
	if got.Repo != "octo/hello-world" || got.Groups[0].PRs[0].Title != "Add search" || !got.FetchedAt.Equal(sample().FetchedAt) {
		t.Fatalf("Load = %+v", got)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	_, ok, err := File{Path: filepath.Join(t.TempDir(), "none.json")}.Load()
	if ok || err != nil {
		t.Fatalf("ok %v, err %v; want a plain miss", ok, err)
	}
}

func TestCorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	_, ok, err := File{Path: path}.Load()
	if ok || err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("ok %v, err %v; want an error naming the file", ok, err)
	}
}

// A cache written by a version with another snapshot schema is ignored
// rather than shown with missing or misread fields.
func TestOtherSchemaVersionIsAMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	os.WriteFile(path, []byte(`{"schema": 999, "repo": "octo/hello-world"}`), 0o644)
	if _, ok, err := (File{Path: path}).Load(); ok || err != nil {
		t.Fatalf("ok %v, err %v; want a plain miss", ok, err)
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	f := File{Path: filepath.Join(dir, "s.json")}
	for range 3 {
		if err := f.Save(sample()); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want only the cache file", len(entries))
	}
}

func TestForPicksAPerRepoAndAuthorFile(t *testing.T) {
	cases := []struct {
		name   string
		vars   map[string]string
		author string
		want   string
	}{
		{"XDG_CACHE_HOME", map[string]string{"XDG_CACHE_HOME": "/xdg", "HOME": "/home/alice"}, "@me", "/xdg/prledger/octo/hello-world/@me.json"},
		{"~/.cache", map[string]string{"HOME": "/home/alice"}, "bob", "/home/alice/.cache/prledger/octo/hello-world/bob.json"},
		{"unsafe author characters", map[string]string{"HOME": "/home/alice"}, "../../etc", "/home/alice/.cache/prledger/octo/hello-world/.._.._etc.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := For(func(k string) string { return c.vars[k] }, repo, c.author)
			if err != nil {
				t.Fatal(err)
			}
			if f.Path != c.want {
				t.Fatalf("Path = %q, want %q", f.Path, c.want)
			}
		})
	}
}
