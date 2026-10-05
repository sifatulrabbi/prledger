package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

type fakeSource struct {
	prs   []core.PR
	err   error
	query core.Query
}

func (f *fakeSource) ListPRs(_ context.Context, q core.Query) ([]core.PR, error) {
	f.query = q
	return f.prs, f.err
}

var (
	now   = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	clock = func() time.Time { return now }
	repo  = core.Repo{Owner: "octo", Name: "hello-world"}
)

func day(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }

func TestSnapshotHoldsEveryPRNewestFirst(t *testing.T) {
	src := &fakeSource{prs: []core.PR{
		{Number: 1, CreatedAt: day(1)},
		{Number: 3, CreatedAt: day(3)},
		{Number: 2, CreatedAt: day(2)},
	}}
	ledger := core.Ledger{Source: src, Now: clock}

	snap, err := ledger.Snapshot(context.Background(), core.Query{Repo: repo, Author: "@me", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Schema != 1 || snap.Repo != "octo/hello-world" || snap.Author != "@me" || !snap.FetchedAt.Equal(now) {
		t.Fatalf("header = %+v", snap)
	}
	if len(snap.Groups) != 1 || snap.Groups[0].Name != "Ungrouped" {
		t.Fatalf("groups = %+v, want one Ungrouped group", snap.Groups)
	}
	var got []int
	for _, pr := range snap.Groups[0].PRs {
		got = append(got, pr.Number)
	}
	if want := []int{3, 2, 1}; !equalInts(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSnapshotWithNoPRsHasNoGroups(t *testing.T) {
	snap, err := core.Ledger{Source: &fakeSource{}, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Groups == nil || len(snap.Groups) != 0 {
		t.Fatalf("groups = %#v, want an empty, non-nil list (JSON [] not null)", snap.Groups)
	}
}

func TestSnapshotPassesSourceErrorsThrough(t *testing.T) {
	boom := errors.New("gh: not logged in")
	_, err := core.Ledger{Source: &fakeSource{err: boom}, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func equalInts(a, b []int) bool {
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
