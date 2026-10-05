// Package cache keeps the last snapshot of each repo on disk, so `serve`
// starts instantly and `list --cached` works offline.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sifatulrabbi/prledger/internal/core"
)

// File is the cache for one repo and author.
type File struct {
	Path string
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9@._-]`)

// For returns the cache file for repo and author under
// $XDG_CACHE_HOME/prledger, else ~/.cache/prledger. account is how gh is
// started (command and environment); "@me" means a different user for each
// gh setup, so each setup gets its own file.
func For(getenv func(string) string, repo core.Repo, author string, account []string) (File, error) {
	base := getenv("XDG_CACHE_HOME")
	if base == "" {
		home := getenv("HOME")
		if home == "" {
			return File{}, errors.New("cannot find the cache folder: HOME is not set")
		}
		base = filepath.Join(home, ".cache")
	}
	sum := sha256.Sum256([]byte(strings.Join(account, "\x00")))
	name := unsafeChars.ReplaceAllString(author, "_") + "-" + hex.EncodeToString(sum[:4])
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

// Save writes the snapshot to a temp file, syncs it and renames it over the
// cache, so readers see the old file or the new one, never a partial write.
// Losing the cache is harmless: the next fetch rebuilds it.
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}
