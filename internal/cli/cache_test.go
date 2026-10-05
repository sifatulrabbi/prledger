package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
	"github.com/sifatulrabbi/prledger/internal/server"
)

func cachePath(h *harness) string {
	return filepath.Join(h.env["XDG_CACHE_HOME"], "prledger", "octo", "hello-world", "@me.json")
}

func TestListCachedWorksWithoutGh(t *testing.T) {
	h := newHarness(t)
	if err := h.run("list", "--json"); err != nil {
		t.Fatal(err)
	}
	fresh := h.stdout.String()

	h.stdout.Reset()
	h.gh.err = errors.New("gh: offline")
	if err := h.run("list", "--json", "--cached"); err != nil {
		t.Fatal(err)
	}
	if got := h.stdout.String(); got != fresh {
		t.Fatalf("cached output differs from the fresh one:\n%s\nvs\n%s", got, fresh)
	}
}

func TestListCachedWithoutACacheExplains(t *testing.T) {
	h := newHarness(t)
	err := h.run("list", "--cached")
	if err == nil || !strings.Contains(err.Error(), "without --cached") {
		t.Fatalf("err = %v, want a hint to run without --cached", err)
	}
}

// Groups are computed from the current config, not the one in force when the
// cache was written.
func TestCachedSnapshotIsRegroupedWithTheCurrentConfig(t *testing.T) {
	h := newHarness(t)
	if err := h.run("list"); err != nil {
		t.Fatal(err)
	}
	h.writeConfig(t, "repos:\n  octo/hello-world:\n    groups:\n      - {name: Picked, prs: [9]}\n")
	h.stdout.Reset()
	if err := h.run("list", "--cached"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout.String(), "Picked (1)") {
		t.Fatalf("table =\n%s\nwant the newly configured group", h.stdout.String())
	}
}

func TestServeStartsFromTheCacheAndReportsRefreshFailures(t *testing.T) {
	h := newHarness(t)
	if err := h.run("list"); err != nil { // writes the cache
		t.Fatal(err)
	}
	h.gh.err = errors.New("gh: rate limited")
	url, _ := startServe(t, h, "--no-open")

	res, err := http.Get(url + "api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	var snap core.Snapshot
	json.NewDecoder(res.Body).Decode(&snap)
	res.Body.Close()
	if res.StatusCode != 200 || snap.Repo != "octo/hello-world" {
		t.Fatalf("GET /api/snapshot = %d %+v, want the cached snapshot", res.StatusCode, snap)
	}

	req, _ := http.NewRequest("POST", url+"api/refresh", nil)
	req.Header.Set(server.RequestHeader, "1")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Error string }
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if res.StatusCode != 502 || !strings.Contains(body.Error, "rate limited") {
		t.Fatalf("POST /api/refresh = %d %+v, want gh's error", res.StatusCode, body)
	}
}

func TestServeWarnsAboutACorruptCache(t *testing.T) {
	h := newHarness(t)
	path := cachePath(h)
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{broken"), 0o644)
	errOut := &syncBuffer{}
	h.deps.Stderr = errOut
	startServe(t, h, "--no-open")
	if !strings.Contains(errOut.String(), "ignoring the cache") {
		t.Fatalf("stderr = %q, want a cache warning", errOut.String())
	}
}
