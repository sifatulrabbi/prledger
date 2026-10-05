package core

import (
	"context"
	"sync"
)

// SnapshotCache stores the last snapshot of one repo between runs.
type SnapshotCache interface {
	// Load returns the cached snapshot; ok is false when there is none.
	Load() (snap Snapshot, ok bool, err error)
	Save(Snapshot) error
}

// Tracker keeps the latest good snapshot of one repo. It starts from the
// cache, refreshes on demand, and shares one fetch between concurrent
// refreshes.
type Tracker struct {
	life  context.Context // fetches run under this, not under any one caller
	fetch func(context.Context) (Snapshot, error)
	cache SnapshotCache

	mu       sync.Mutex
	current  *Snapshot
	cacheErr error
	inflight *flight
}

type flight struct {
	done chan struct{}
	snap Snapshot
	err  error
}

// NewTracker loads the cache and returns a Tracker. Fetches run under life, so
// cancelling it stops them. A cache that fails to load is not fatal;
// CacheError reports it.
func NewTracker(life context.Context, fetch func(context.Context) (Snapshot, error), cache SnapshotCache) *Tracker {
	t := &Tracker{life: life, fetch: fetch, cache: cache}
	snap, ok, err := cache.Load()
	switch {
	case err != nil:
		t.cacheErr = err
	case ok:
		t.current = &snap
	}
	return t
}

// CacheError is the last error loading or saving the cache, if any.
func (t *Tracker) CacheError() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cacheErr
}

// Current returns the latest good snapshot, fetching one if none is known.
func (t *Tracker) Current(ctx context.Context) (Snapshot, error) {
	t.mu.Lock()
	cur := t.current
	t.mu.Unlock()
	if cur != nil {
		return *cur, nil
	}
	return t.Refresh(ctx)
}

// Refresh fetches a new snapshot. Callers that arrive while a fetch runs wait
// for that same fetch. On failure the last good snapshot is kept.
func (t *Tracker) Refresh(ctx context.Context) (Snapshot, error) {
	t.mu.Lock()
	f := t.inflight
	if f == nil {
		f = &flight{done: make(chan struct{})}
		t.inflight = f
		go t.run(f)
	}
	t.mu.Unlock()
	select {
	case <-f.done:
		return f.snap, f.err
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

func (t *Tracker) run(f *flight) {
	// Under the tracker's life, not a caller's: one caller leaving must not
	// cancel the fetch the others are waiting on.
	snap, err := t.fetch(t.life)
	var saveErr error
	if err == nil {
		saveErr = t.cache.Save(snap)
	}

	t.mu.Lock()
	if err == nil {
		t.current = &snap
		t.cacheErr = saveErr
	}
	t.inflight = nil
	t.mu.Unlock()

	f.snap, f.err = snap, err
	close(f.done)
}
