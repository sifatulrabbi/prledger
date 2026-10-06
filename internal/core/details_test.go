package core_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func TestSummarizeChecks(t *testing.T) {
	const pass, fail, pending = core.CheckPass, core.CheckFail, core.CheckPending
	tests := []struct {
		name   string
		states []core.CheckState
		want   *core.Checks
	}{
		{"no checks", nil, nil},
		{"all pass", []core.CheckState{pass, pass}, &core.Checks{State: pass, Passed: 2}},
		{"one failure fails the PR", []core.CheckState{pass, pending, fail}, &core.Checks{State: fail, Passed: 1, Failed: 1, Pending: 1}},
		{"pending without failures", []core.CheckState{pass, pending}, &core.Checks{State: pending, Passed: 1, Pending: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := core.SummarizeChecks(tt.states); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SummarizeChecks = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// detailed snapshots one PR with the given details and returns it.
func detailed(t *testing.T, pr core.PR, d core.Details) core.PR {
	t.Helper()
	d.Number = pr.Number
	src := &fakeSource{prs: []core.PR{pr}, details: []core.Details{d}}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	return snap.PRs()[0]
}

var openPR = core.PR{Number: 7, Status: core.StatusOpen, CreatedAt: day(1)}

func TestReviewersJoinRequestsAndReviews(t *testing.T) {
	got := detailed(t, openPR, core.Details{
		Requested: []string{"carol", "octo/backend"},
		Reviews: []core.Reviewer{
			{Login: "bob", State: core.ReviewerApproved},
			{Login: "carol", State: core.ReviewerChanges}, // asked again since
		},
	})
	want := []core.Reviewer{
		{Login: "bob", State: core.ReviewerApproved},
		{Login: "carol", State: core.ReviewerRequested},
		{Login: "octo/backend", State: core.ReviewerRequested},
	}
	if !reflect.DeepEqual(got.Reviewers, want) {
		t.Fatalf("Reviewers = %+v\nwant        %+v", got.Reviewers, want)
	}
}

func TestReviewState(t *testing.T) {
	approved := core.Reviewer{Login: "bob", State: core.ReviewerApproved}
	changes := core.Reviewer{Login: "carol", State: core.ReviewerChanges}
	tests := []struct {
		name string
		d    core.Details
		want core.Review
	}{
		{"GitHub's decision wins", core.Details{Decision: core.ReviewRequired, Reviews: []core.Reviewer{approved}}, core.ReviewRequired},
		{"no rule: an approval approves", core.Details{Reviews: []core.Reviewer{approved}}, core.ReviewApproved},
		{"no rule: changes requested beat approvals", core.Details{Reviews: []core.Reviewer{approved, changes}}, core.ReviewChanges},
		{"no rule: a pending request needs review", core.Details{Requested: []string{"dan"}}, core.ReviewRequired},
		// GitHub keeps CHANGES_REQUESTED until that reviewer approves; once
		// the author asked them again, the PR waits on them.
		{"changes requested, then asked again", core.Details{Decision: core.ReviewChanges, Reviews: []core.Reviewer{approved, changes}, Requested: []string{"carol"}}, core.ReviewRequired},
		{"changes requested, one of two asked again", core.Details{Decision: core.ReviewChanges, Reviews: []core.Reviewer{changes, {Login: "erin", State: core.ReviewerChanges}}, Requested: []string{"carol"}}, core.ReviewChanges},
		{"no rule and no reviewers", core.Details{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detailed(t, openPR, tt.d).Review; got != tt.want {
				t.Fatalf("Review = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttention(t *testing.T) {
	failing := &core.Checks{State: core.CheckFail, Failed: 1}
	passing := &core.Checks{State: core.CheckPass, Passed: 1}
	draft := openPR
	draft.Status = core.StatusDraft
	tests := []struct {
		name string
		pr   core.PR
		d    core.Details
		want core.Attention
	}{
		{"conflicts need you", openPR, core.Details{Merge: core.MergeConflicting, Decision: core.ReviewApproved}, core.AttentionNeedsYou},
		{"failing CI needs you", openPR, core.Details{Checks: failing}, core.AttentionNeedsYou},
		{"requested changes need you", openPR, core.Details{Decision: core.ReviewChanges}, core.AttentionNeedsYou},
		{"approved and green", openPR, core.Details{Decision: core.ReviewApproved, Checks: passing}, core.AttentionApproved},
		{"open and quiet is waiting", openPR, core.Details{Decision: core.ReviewRequired, Checks: passing}, core.AttentionWaiting},
		{"a draft with failing CI needs you", draft, core.Details{Checks: failing}, core.AttentionNeedsYou},
		{"a quiet draft asks nothing", draft, core.Details{Decision: core.ReviewApproved}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detailed(t, tt.pr, tt.d).Attention; got != tt.want {
				t.Fatalf("Attention = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetailsOnlyReachTheirPR(t *testing.T) {
	other := core.PR{Number: 8, Status: core.StatusOpen, CreatedAt: day(2)}
	src := &fakeSource{prs: []core.PR{openPR, other}, details: []core.Details{{Number: 7, Merge: core.MergeConflicting}}}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range snap.PRs() {
		if p.Number == 8 && (p.Merge != "" || p.Attention != "") {
			t.Fatalf("PR #8 = %+v, want no details: gh sent none for it", p)
		}
		if p.Number == 7 && p.Merge != core.MergeConflicting {
			t.Fatalf("PR #7 = %+v, want its details", p)
		}
	}
}

// Reviews and CI are extras: when gh cannot give them the PRs still list,
// and the snapshot says what is missing.
func TestFailedDetailsBecomeAWarning(t *testing.T) {
	src := &fakeSource{prs: []core.PR{openPR}, detailsErr: errors.New("HTTP 504: Gateway Timeout")}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.PRs()) != 1 || len(snap.Warnings) != 1 || !strings.Contains(snap.Warnings[0], "504") {
		t.Fatalf("snap = %+v, want the PR and one warning naming the failure", snap)
	}
}

// Regression: GitHub sometimes cuts the slow details response short (gh says
// "unexpected end of JSON input"); one retry hides most of those.
func TestDetailsAreRetriedOnce(t *testing.T) {
	src := &fakeSource{prs: []core.PR{openPR}, details: []core.Details{{Number: 7, Merge: core.MergeBehind}}, detailsErr: errors.New("gh: unexpected end of JSON input"), detailsFailures: 1}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil || src.detailsCalls != 2 || len(snap.Warnings) != 0 || snap.PRs()[0].Merge != core.MergeBehind {
		t.Fatalf("calls %d, warnings %q, PR %+v, err %v; want the second try's details", src.detailsCalls, snap.Warnings, snap.PRs()[0], err)
	}
}

// Regression: Ctrl-C while the details call ran gave a "successful"
// snapshot without details, which list printed and the cache kept.
func TestCancelDuringDetailsFailsTheSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &fakeSource{prs: []core.PR{openPR}, detailsFn: func(ctx context.Context) ([]core.Details, error) {
		cancel() // Ctrl-C while gh runs
		return nil, errors.New("gh: signal: killed")
	}}
	_, err := core.Ledger{Source: src, Now: clock}.Snapshot(ctx, core.Query{Repo: repo})
	if !errors.Is(err, context.Canceled) || src.detailsCalls != 1 {
		t.Fatalf("err = %v after %d calls, want context.Canceled after 1", err, src.detailsCalls)
	}
}

// A details call that hangs runs out its own time, not the whole fetch's,
// and is tried again in fresh time.
func TestSlowDetailsTimeOutIntoAWarning(t *testing.T) {
	src := &fakeSource{prs: []core.PR{openPR}, detailsFn: func(ctx context.Context) ([]core.Details, error) {
		<-ctx.Done()
		return nil, errors.New("gh: signal: killed")
	}}
	l := core.Ledger{Source: src, Now: clock, DetailsTimeout: 10 * time.Millisecond}
	snap, err := l.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil || src.detailsCalls != 2 || len(snap.Warnings) != 1 || !strings.Contains(snap.Warnings[0], "took longer than") {
		t.Fatalf("err %v, calls %d, warnings %q; want two timed-out tries and a warning", err, src.detailsCalls, snap.Warnings)
	}
}

func TestSkipDetailsMakesNoDetailsCall(t *testing.T) {
	src := &fakeSource{prs: []core.PR{openPR}, detailsErr: errors.New("must not be called")}
	snap, err := core.Ledger{Source: src, Now: clock, SkipDetails: true}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil || src.detailsCalls != 0 || len(snap.Warnings) != 0 {
		t.Fatalf("details calls = %d, warnings %q, err %v", src.detailsCalls, snap.Warnings, err)
	}
}

func TestNoOpenPRsSkipTheDetailsCall(t *testing.T) {
	merged := core.PR{Number: 1, Status: core.StatusMerged, CreatedAt: day(1)}
	src := &fakeSource{prs: []core.PR{merged}, detailsErr: errors.New("must not be called")}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil || src.detailsCalls != 0 || len(snap.Warnings) != 0 {
		t.Fatalf("details calls = %d, warnings %q, err %v", src.detailsCalls, snap.Warnings, err)
	}
}

func TestRegroupKeepsWarnings(t *testing.T) {
	s := core.Snapshot{Warnings: []string{"no checks"}, Groups: []core.Group{}}
	if got := (core.Ledger{}).Regroup(s); !reflect.DeepEqual(got.Warnings, s.Warnings) {
		t.Fatalf("Warnings = %q", got.Warnings)
	}
}
