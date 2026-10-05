package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

type fakeSnapshots struct {
	snap       core.Snapshot
	err        error
	refreshed  core.Snapshot
	refreshErr error
}

func (f fakeSnapshots) Current(context.Context) (core.Snapshot, error) { return f.snap, f.err }
func (f fakeSnapshots) Refresh(context.Context) (core.Snapshot, error) {
	return f.refreshed, f.refreshErr
}

func TestRefreshReturnsTheNewSnapshot(t *testing.T) {
	fresh := sample
	fresh.Repo = "octo/refreshed"
	res := do(t, New(fakeSnapshots{snap: sample, refreshed: fresh}, []byte(page)), "POST", "/api/refresh")
	var got core.Snapshot
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != 200 || got.Repo != "octo/refreshed" {
		t.Fatalf("status %d, snapshot %+v, err %v", res.StatusCode, got, err)
	}
}

func TestRefreshFailureIsAJSONError(t *testing.T) {
	res := do(t, New(fakeSnapshots{snap: sample, refreshErr: errors.New("gh: rate limited")}, []byte(page)), "POST", "/api/refresh")
	var got struct{ Error string }
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != 502 || got.Error != "gh: rate limited" {
		t.Fatalf("status %d, body %+v, err %v", res.StatusCode, got, err)
	}
}

// Another site's form can POST to 127.0.0.1, but it cannot add a custom
// header without a CORS preflight, which the server never answers.
func TestRefreshNeedsThePrledgerHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/refresh", nil)
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	New(fakeSnapshots{snap: sample}, []byte(page)).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without %s = %d, want 403", RequestHeader, rec.Code)
	}
}

// Refresh runs gh, so it must not be triggerable by a plain GET (a link or
// an <img> on another page).
func TestRefreshNeedsPOST(t *testing.T) {
	if res := do(t, New(fakeSnapshots{snap: sample}, []byte(page)), "GET", "/api/refresh"); res.StatusCode != 405 {
		t.Fatalf("GET /api/refresh = %d, want 405", res.StatusCode)
	}
}

var sample = core.Snapshot{
	Schema: 1, Repo: "octo/hello-world", Author: "@me",
	FetchedAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
	Groups:    []core.Group{{Name: "Ungrouped", PRs: []core.PR{{Number: 7, Title: "Add search", Status: core.StatusOpen}}}},
}

const page = `<html><script id="prledger-data" type="application/json"></script></html>`

func do(t *testing.T, h http.Handler, method, target string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = "127.0.0.1:4321"
	if method == "POST" {
		req.Header.Set(RequestHeader, "1")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRootServesThePage(t *testing.T) {
	res := do(t, New(fakeSnapshots{snap: sample}, []byte(page)), "GET", "/")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if got := body(t, res); got != page {
		t.Fatalf("body = %q", got)
	}
}

func TestSnapshotEndpointReturnsTheContract(t *testing.T) {
	res := do(t, New(fakeSnapshots{snap: sample}, []byte(page)), "GET", "/api/snapshot")
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	var got core.Snapshot
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Repo != "octo/hello-world" || len(got.Groups) != 1 || got.Groups[0].PRs[0].Number != 7 {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestSnapshotFailureIsAJSONError(t *testing.T) {
	res := do(t, New(fakeSnapshots{err: errors.New("gh: not logged in")}, []byte(page)), "GET", "/api/snapshot")
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.StatusCode)
	}
	var got struct{ Error string }
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || got.Error != "gh: not logged in" {
		t.Fatalf("body = %+v (%v)", got, err)
	}
}

func TestUnknownPathsAndMethods(t *testing.T) {
	h := New(fakeSnapshots{snap: sample}, []byte(page))
	if res := do(t, h, "GET", "/nope"); res.StatusCode != 404 {
		t.Errorf("GET /nope = %d, want 404", res.StatusCode)
	}
	if res := do(t, h, "DELETE", "/api/snapshot"); res.StatusCode != 405 {
		t.Errorf("DELETE /api/snapshot = %d, want 405", res.StatusCode)
	}
}

// A web page on another site can point a hostname at 127.0.0.1 (DNS
// rebinding) and read the API from the browser. Only loopback Host headers
// are served.
func TestForeignHostHeadersAreRefused(t *testing.T) {
	h := New(fakeSnapshots{snap: sample}, []byte(page))
	for host, want := range map[string]int{
		"127.0.0.1:4321":  200,
		"localhost:4321":  200,
		"[::1]:4321":      200,
		"evil.example":    403,
		"evil.example:80": 403,
	} {
		req := httptest.NewRequest("GET", "/api/snapshot", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: status %d, want %d", host, rec.Code, want)
		}
	}
}
