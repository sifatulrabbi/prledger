package core_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
)

type memCache struct {
	mu    sync.Mutex
	snap  *core.Snapshot
	err   error
	saves int
}

func (m *memCache) Load() (core.Snapshot, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return core.Snapshot{}, false, m.err
	}
	if m.snap == nil {
		return core.Snapshot{}, false, nil
	}
	return *m.snap, true, nil
}

func (m *memCache) Save(s core.Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snap = &s
	m.saves++
	return nil
}

// counter is a fetch function that returns numbered snapshots.
type counter struct {
	calls atomic.Int32
	err   error
	gate  chan struct{} // when set, fetch waits for it to close
}

func (c *counter) fetch(ctx context.Context) (core.Snapshot, error) {
	n := c.calls.Add(1)
	if c.gate != nil {
		<-c.gate
	}
	if c.err != nil {
		return core.Snapshot{}, c.err
	}
	return core.Snapshot{Repo: "octo/hello-world", FetchedAt: now.Add(time.Duration(n) * time.Minute)}, nil
}

func TestCurrentFetchesWhenNothingIsKnown(t *testing.T) {
	c := &counter{}
	tr := core.NewTracker(context.Background(), c.fetch, &memCache{})
	snap, err := tr.Current(context.Background())
	if err != nil || snap.Repo != "octo/hello-world" || c.calls.Load() != 1 {
		t.Fatalf("snap=%+v err=%v calls=%d", snap, err, c.calls.Load())
	}
	if _, err := tr.Current(context.Background()); err != nil || c.calls.Load() != 1 {
		t.Fatalf("second Current fetched again (calls=%d, err=%v)", c.calls.Load(), err)
	}
}

func TestCurrentStartsFromTheCache(t *testing.T) {
	cached := core.Snapshot{Repo: "octo/hello-world", FetchedAt: now}
	c := &counter{}
	tr := core.NewTracker(context.Background(), c.fetch, &memCache{snap: &cached})
	snap, err := tr.Current(context.Background())
	if err != nil || !snap.FetchedAt.Equal(now) || c.calls.Load() != 0 {
		t.Fatalf("snap=%+v err=%v calls=%d, want the cached snapshot without fetching", snap, err, c.calls.Load())
	}
}

func TestCorruptCacheIsReportedButNotFatal(t *testing.T) {
	c := &counter{}
	tr := core.NewTracker(context.Background(), c.fetch, &memCache{err: errors.New("bad json")})
	if err := tr.CacheError(); err == nil {
		t.Fatal("CacheError() = nil, want the load error")
	}
	if _, err := tr.Current(context.Background()); err != nil || c.calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d, want a fetch instead", err, c.calls.Load())
	}
}

func TestRefreshSavesToTheCache(t *testing.T) {
	cache := &memCache{}
	tr := core.NewTracker(context.Background(), (&counter{}).fetch, cache)
	if _, err := tr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cache.saves != 1 || cache.snap == nil {
		t.Fatalf("saves = %d, want 1", cache.saves)
	}
}

func TestConcurrentRefreshesShareOneFetch(t *testing.T) {
	c := &counter{gate: make(chan struct{})}
	tr := core.NewTracker(context.Background(), c.fetch, &memCache{})
	var wg sync.WaitGroup
	results := make([]core.Snapshot, 5)
	for i := range results {
		wg.Go(func() {
			s, err := tr.Refresh(context.Background())
			if err != nil {
				t.Error(err)
			}
			results[i] = s
		})
	}
	// Wait until the first fetch is running, then let the others pile up.
	for c.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(c.gate)
	wg.Wait()
	if got := c.calls.Load(); got != 1 {
		t.Fatalf("fetch ran %d times, want 1", got)
	}
	for _, s := range results {
		if !s.FetchedAt.Equal(results[0].FetchedAt) {
			t.Fatalf("callers got different snapshots: %v", results)
		}
	}
}

func TestFailedRefreshKeepsTheLastGoodSnapshot(t *testing.T) {
	c := &counter{}
	tr := core.NewTracker(context.Background(), c.fetch, &memCache{})
	good, err := tr.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.err = errors.New("gh: rate limited")
	if _, err := tr.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh error = nil, want gh's error")
	}
	snap, err := tr.Current(context.Background())
	if err != nil || !snap.FetchedAt.Equal(good.FetchedAt) {
		t.Fatalf("Current = %+v, %v; want the last good snapshot", snap, err)
	}
}

// The tracker's own context (serve's lifetime) does cancel a running fetch,
// so Ctrl-C stops gh.
func TestCancellingTheTrackerContextStopsTheFetch(t *testing.T) {
	life, stop := context.WithCancel(context.Background())
	started := make(chan struct{})
	tr := core.NewTracker(life, func(ctx context.Context) (core.Snapshot, error) {
		close(started)
		<-ctx.Done()
		return core.Snapshot{}, ctx.Err()
	}, &memCache{})
	errc := make(chan error, 1)
	go func() { _, err := tr.Refresh(context.Background()); errc <- err }()
	<-started
	stop()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fetch kept running after the tracker context was cancelled")
	}
}

// Regression: with no time limit a hung gh (network stall, keychain prompt)
// kept every later refresh waiting on it until Ctrl-C.
func TestAHungFetchTimesOutAndTheNextRefreshStartsAfresh(t *testing.T) {
	var calls atomic.Int32
	tr := core.NewTracker(context.Background(), func(ctx context.Context) (core.Snapshot, error) {
		calls.Add(1)
		<-ctx.Done()
		return core.Snapshot{}, ctx.Err()
	}, &memCache{})
	tr.FetchTimeout = 30 * time.Millisecond

	if _, err := tr.Refresh(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	tr.Refresh(context.Background())
	if got := calls.Load(); got != 2 {
		t.Fatalf("fetch ran %d times, want a new fetch after the timeout", got)
	}
}

// A fetch must not be cancelled because the first caller went away: the
// others are still waiting for it.
func TestRefreshOutlivesTheCallerThatStartedIt(t *testing.T) {
	c := &counter{gate: make(chan struct{})}
	tr := core.NewTracker(context.Background(), func(ctx context.Context) (core.Snapshot, error) {
		s, err := c.fetch(ctx)
		if ctx.Err() != nil {
			return core.Snapshot{}, ctx.Err()
		}
		return s, err
	}, &memCache{})

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := tr.Refresh(ctx); first <- err }()
	for c.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	second := make(chan error, 1)
	go func() { _, err := tr.Refresh(context.Background()); second <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller err = %v, want context.Canceled", err)
	}
	close(c.gate)
	if err := <-second; err != nil {
		t.Fatalf("second caller err = %v, want the shared fetch to finish", err)
	}
}
