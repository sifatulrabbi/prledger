package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func at(h int) time.Time { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC) }

// discussed snapshots one PR with quiet details and the given discussion.
func discussed(t *testing.T, pr core.PR, talk core.Discussion) core.PR {
	t.Helper()
	talk.Number = pr.Number
	src := &fakeSource{prs: []core.PR{pr}, details: []core.Details{{Number: pr.Number}}, discussions: []core.Discussion{talk}}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	return snap.PRs()[0]
}

var authoredPR = core.PR{Number: 7, Status: core.StatusOpen, Author: "me", CreatedAt: day(1)}

func TestCommentersCountEachPersonNewestFirst(t *testing.T) {
	got := discussed(t, authoredPR, core.Discussion{
		Notes: []core.Note{
			{Login: "alice", At: at(1)},
			{Login: "bob", At: at(2)},
			{Login: "me", At: at(3)}, // the author answering is not feedback
			{Login: "alice", At: at(4)},
		},
	})
	want := []core.Commenter{
		{Login: "alice", Comments: 2, LastAt: at(4)},
		{Login: "bob", Comments: 1, LastAt: at(2)},
	}
	if !reflect.DeepEqual(got.Commenters, want) {
		t.Fatalf("Commenters = %+v\nwant         %+v", got.Commenters, want)
	}
}

func TestUnresolvedThreadsBelongToWhoeverOpenedThem(t *testing.T) {
	got := discussed(t, authoredPR, core.Discussion{
		Notes: []core.Note{{Login: "alice", At: at(1)}, {Login: "bob", At: at(2)}, {Login: "carol", At: at(3)}},
		Threads: []core.Thread{
			{Opener: "alice"},
			{Opener: "alice"},
			{Opener: "alice", Resolved: true},
			// Regression (review): replies made a reviewer own the author's
			// note to self, and a human reply to a bot's thread made the
			// human its owner. Neither asks the author anything.
			{Opener: "me"},
			{Opener: ""}, // a bot or a deleted account
		},
	})
	want := []core.Commenter{
		{Login: "carol", Comments: 1, LastAt: at(3)},
		{Login: "bob", Comments: 1, LastAt: at(2)},
		{Login: "alice", Comments: 1, Unresolved: 2, LastAt: at(1)},
	}
	if !reflect.DeepEqual(got.Commenters, want) {
		t.Fatalf("Commenters = %+v\nwant         %+v", got.Commenters, want)
	}
}

// The adapter adds a note for every thread comment it reads, but core takes
// any input: an opener without notes has no last time, and JSON leaves it out
// rather than claiming the year 1.
func TestAThreadOpenerWithoutNotesHasNoTime(t *testing.T) {
	got := discussed(t, authoredPR, core.Discussion{Threads: []core.Thread{{Opener: "alice"}}})
	if want := []core.Commenter{{Login: "alice", Unresolved: 1}}; !reflect.DeepEqual(got.Commenters, want) {
		t.Fatalf("Commenters = %+v, want %+v", got.Commenters, want)
	}
	if b, _ := json.Marshal(got.Commenters[0]); strings.Contains(string(b), "lastAt") {
		t.Fatalf("JSON = %s, want no lastAt", b)
	}
}

func TestUnresolvedThreadsNeedYou(t *testing.T) {
	draft := authoredPR
	draft.Status = core.StatusDraft
	open := core.Discussion{Notes: []core.Note{{Login: "alice", At: at(1)}}, Threads: []core.Thread{{Opener: "alice"}}}
	tests := []struct {
		name string
		pr   core.PR
		talk core.Discussion
		want core.Attention
	}{
		{"an unresolved thread needs you", authoredPR, open, core.AttentionNeedsYou},
		{"on a draft too", draft, open, core.AttentionNeedsYou},
		{"resolved threads do not", authoredPR, core.Discussion{Threads: []core.Thread{{Opener: "alice", Resolved: true}}}, core.AttentionWaiting},
		// Plain comments have no resolved state, so they cannot say whether
		// the author still owes an answer.
		{"plain comments do not", authoredPR, core.Discussion{Notes: []core.Note{{Login: "alice", At: at(1)}}}, core.AttentionWaiting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := discussed(t, tt.pr, tt.talk).Attention; got != tt.want {
				t.Fatalf("Attention = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFailedDiscussionsBecomeAWarning(t *testing.T) {
	src := &fakeSource{prs: []core.PR{openPR}, details: []core.Details{{Number: 7, Merge: core.MergeBehind}}, discussionsErr: errors.New("HTTP 502")}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if src.discussionsCalls != 2 || len(snap.Warnings) != 1 || !strings.Contains(snap.Warnings[0], "Review comments") || !strings.Contains(snap.Warnings[0], "502") {
		t.Fatalf("calls %d, warnings %q; want two tries and one warning about comments", src.discussionsCalls, snap.Warnings)
	}
	if p := snap.PRs()[0]; p.Merge != core.MergeBehind || p.Attention != core.AttentionWaiting {
		t.Fatalf("PR = %+v, want its details kept", p)
	}
}

// Who commented means little without the review and CI state, so a PR whose
// details failed gets no attention from comments alone.
func TestDiscussionsWithoutDetailsSetNoAttention(t *testing.T) {
	src := &fakeSource{prs: []core.PR{authoredPR}, detailsErr: errors.New("HTTP 504"),
		discussions: []core.Discussion{{Number: 7, Notes: []core.Note{{Login: "alice", At: at(1)}}, Threads: []core.Thread{{Opener: "alice"}}}}}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	p := snap.PRs()[0]
	if len(p.Commenters) != 1 || p.Attention != "" || len(snap.Warnings) != 1 {
		t.Fatalf("PR = %+v, warnings %q; want the commenter, no attention, one warning", p, snap.Warnings)
	}
}

// The discussions call gets the same care as the details call: one retry,
// its own time per try, and no half snapshot when cancelled.
func TestDiscussionsAreRetriedOnce(t *testing.T) {
	src := &fakeSource{prs: []core.PR{authoredPR}, details: []core.Details{{Number: 7}}, discussionsErr: errors.New("gh: unexpected end of JSON input"), discussionsFailures: 1,
		discussions: []core.Discussion{{Number: 7, Notes: []core.Note{{Login: "alice", At: at(1)}}}}}
	snap, err := core.Ledger{Source: src, Now: clock}.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil || src.discussionsCalls != 2 || len(snap.Warnings) != 0 || len(snap.PRs()[0].Commenters) != 1 {
		t.Fatalf("calls %d, warnings %q, PR %+v, err %v; want the second try's comments", src.discussionsCalls, snap.Warnings, snap.PRs()[0], err)
	}
}

func TestSlowDiscussionsTimeOutIntoAWarning(t *testing.T) {
	src := &fakeSource{prs: []core.PR{openPR}, details: []core.Details{{Number: 7}}, discussionsFn: func(ctx context.Context) ([]core.Discussion, error) {
		<-ctx.Done()
		return nil, errors.New("gh: signal: killed")
	}}
	l := core.Ledger{Source: src, Now: clock, DetailsTimeout: 10 * time.Millisecond}
	snap, err := l.Snapshot(context.Background(), core.Query{Repo: repo})
	if err != nil || src.discussionsCalls != 2 || len(snap.Warnings) != 1 || !strings.Contains(snap.Warnings[0], "Review comments are not shown: gh took longer than") {
		t.Fatalf("err %v, calls %d, warnings %q; want two timed-out tries and a warning", err, src.discussionsCalls, snap.Warnings)
	}
}

func TestCancelDuringDiscussionsFailsTheSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &fakeSource{prs: []core.PR{openPR}, details: []core.Details{{Number: 7}}, discussionsFn: func(context.Context) ([]core.Discussion, error) {
		cancel() // Ctrl-C while gh runs
		return nil, errors.New("gh: signal: killed")
	}}
	_, err := core.Ledger{Source: src, Now: clock}.Snapshot(ctx, core.Query{Repo: repo})
	if !errors.Is(err, context.Canceled) || src.discussionsCalls != 1 {
		t.Fatalf("err = %v after %d calls, want context.Canceled after 1", err, src.discussionsCalls)
	}
}

func TestSkipDetailsMakesNoDiscussionsCall(t *testing.T) {
	src := &fakeSource{prs: []core.PR{openPR}, discussionsErr: errors.New("must not be called")}
	if _, err := (core.Ledger{Source: src, Now: clock, SkipDetails: true}).Snapshot(context.Background(), core.Query{Repo: repo}); err != nil || src.discussionsCalls != 0 {
		t.Fatalf("discussions calls = %d, err %v", src.discussionsCalls, err)
	}
}
