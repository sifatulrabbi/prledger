package web

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

var dataTag = regexp.MustCompile(`(?s)<script id="prledger-data" type="application/json">(.*?)</script>`)

func snapshotTitled(title string) core.Snapshot {
	return core.Snapshot{
		Schema: core.SchemaVersion, Repo: "octo/hello-world", Author: "@me",
		FetchedAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
		Groups:    []core.Group{{Name: "Ungrouped", PRs: []core.PR{{Number: 1, Title: title, Status: core.StatusOpen}}}},
	}
}

// Export depends on this tag; if the page loses it, Export must not silently
// produce a page without data.
func TestPageHasOneEmptyDataTag(t *testing.T) {
	m := dataTag.FindAllSubmatch(Page(), -1)
	if len(m) != 1 || len(bytes.TrimSpace(m[0][1])) != 0 {
		t.Fatalf("found %d data tags (want exactly one, empty)", len(m))
	}
}

// Regression: the exported page showed a Refresh button with no server behind
// it, because a display rule on .btn beat the hidden attribute.
func TestHiddenAttributeAlwaysHides(t *testing.T) {
	if !strings.Contains(string(Page()), "[hidden] { display: none !important; }") {
		t.Fatal("page lost its [hidden] rule; elements toggled with .hidden would show")
	}
}

// Card links come from snapshot data, which an exported file lets anyone
// edit; the page must only link https URLs, never javascript: ones.
func TestCardLinksAreHTTPSOnly(t *testing.T) {
	if !strings.Contains(string(Page()), `a.href = /^https:\/\//.test(p.url) ? p.url : "#";`) {
		t.Fatal("card links no longer check for https")
	}
}

func TestExportEmbedsTheSnapshot(t *testing.T) {
	out, err := Export(snapshotTitled("Add search"))
	if err != nil {
		t.Fatal(err)
	}
	m := dataTag.FindSubmatch(out)
	if m == nil {
		t.Fatal("no data tag in the export")
	}
	var got core.Snapshot
	if err := json.Unmarshal(m[1], &got); err != nil {
		t.Fatal(err)
	}
	if got.Repo != "octo/hello-world" || got.Groups[0].PRs[0].Title != "Add search" {
		t.Fatalf("embedded = %+v", got)
	}
}

// A PR title is untrusted text. It must not be able to end the data tag and
// run script in the exported page.
func TestExportedTitlesCannotBreakOutOfTheDataTag(t *testing.T) {
	evil := `</script><script>alert("pwned")</script><!--`
	out, err := Export(snapshotTitled(evil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `<script>alert("pwned")`) {
		t.Fatal("title was embedded unescaped")
	}
	var got core.Snapshot
	if err := json.Unmarshal(dataTag.FindSubmatch(out)[1], &got); err != nil {
		t.Fatal(err)
	}
	if got.Groups[0].PRs[0].Title != evil {
		t.Fatalf("title = %q, want it intact after decoding", got.Groups[0].PRs[0].Title)
	}
}
