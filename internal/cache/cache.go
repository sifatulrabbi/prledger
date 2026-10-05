// Package cache keeps the last snapshot of each repo on disk, so `serve`
// starts instantly and `list --cached` works offline.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// File is the cache for one repo and author.
type File struct {
	Path string
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9@._-]`)

// For returns the cache file for repo and author under
// $XDG_CACHE_HOME/prledger, else ~/.cache/prledger.
func For(getenv func(string) string, repo core.Repo, author string) (File, error) {
	base := getenv("XDG_CACHE_HOME")
	if base == "" {
		home := getenv("HOME")
		if home == "" {
			return File{}, errors.New("cannot find the cache folder: HOME is not set")
		}
		base = filepath.Join(home, ".cache")
	}
	name := unsafeChars.ReplaceAllString(author, "_")
	parts := []string{base, "prledger", unsafeChars.ReplaceAllString(repo.Owner, "_"), unsafeChars.ReplaceAllString(repo.Name, "_"), name + ".json"}
	return File{Path: filepath.Join(parts...)}, nil
}

// Load reads the cached snapshot. A missing file, or one written with another
// schema version, is a miss rather than an error.
func (f File) Load() (core.Snapshot, bool, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return core.Snapshot{}, false, nil
	}
	if err != nil {
		return core.Snapshot{}, false, err
	}
	var snap core.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return core.Snapshot{}, false, fmt.Errorf("cache %s: %w", f.Path, err)
	}
	if snap.Schema != core.SchemaVersion {
		return core.Snapshot{}, false, nil
	}
	return snap, true, nil
}

// Save writes the snapshot atomically: to a temp file, then renamed over the
// cache, so a crash never leaves half a file behind.
func (f File) Save(snap core.Snapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".snapshot-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}
