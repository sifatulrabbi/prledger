package core

import (
	"context"
	"sync"
	"time"
)

// SnapshotCache stores the last snapshot of one repo between runs.
type SnapshotCache interface {
	// Load returns the cached snapshot; ok is false when there is none.
	Load() (snap Snapshot, ok bool, err error)
	Save(Snapshot) error
}

// fetchTimeout bounds one shared fetch, which runs detached from the callers
// waiting on it.
const fetchTimeout = 2 * time.Minute

// Tracker keeps the latest good snapshot of one repo. It starts from the
// cache, refreshes on demand, and shares one fetch between concurrent
// refreshes.
type Tracker struct {
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

// NewTracker loads the cache and returns a Tracker. A cache that fails to load
// is not fatal; CacheError reports it.
func NewTracker(fetch func(context.Context) (Snapshot, error), cache SnapshotCache) *Tracker {
	t := &Tracker{fetch: fetch, cache: cache}
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
	// Detached from any caller: one leaving must not cancel the others' fetch.
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	snap, err := t.fetch(ctx)
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
