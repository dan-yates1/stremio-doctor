// Package watch rescans on an interval and reports what changed.
package watch

import (
	"context"
	"sync"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/history"
	"github.com/dan-yates1/stremio-doctor/internal/report"
)

// ScanFunc runs one full scan.
type ScanFunc func(ctx context.Context) (report.Report, error)

// Cycle is the outcome of one scan.
type Cycle struct {
	N       int // 1 for the first scan
	Report  report.Report
	Entry   history.Entry
	Changes []history.Change // status changes since the previous successful scan
	Err     error            // the scan failed; Report and Entry are empty
}

// Watcher runs scans until its context ends. Its methods are safe to call
// from other goroutines (e.g. a tray menu).
type Watcher struct {
	interval time.Duration
	scan     ScanFunc
	prev     *history.Entry
	trigger  chan struct{}

	mu     sync.Mutex
	paused bool
}

// New returns a watcher. prev, if non-nil, is the last scan from an earlier
// session, so the first cycle can already report changes.
func New(interval time.Duration, scan ScanFunc, prev *history.Entry) *Watcher {
	return &Watcher{interval: interval, scan: scan, prev: prev, trigger: make(chan struct{}, 1)}
}

// ScanNow asks for a scan as soon as the current one (if any) finishes. It
// works while paused.
func (w *Watcher) ScanNow() {
	select {
	case w.trigger <- struct{}{}:
	default: // one is already queued
	}
}

// SetPaused stops or resumes interval scans.
func (w *Watcher) SetPaused(p bool) {
	w.mu.Lock()
	w.paused = p
	w.mu.Unlock()
}

// Paused reports whether interval scans are stopped.
func (w *Watcher) Paused() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paused
}

// Run scans immediately (even when paused), then every interval measured
// from the end of the previous scan, calling on after each one. It returns
// when ctx ends.
func (w *Watcher) Run(ctx context.Context, on func(Cycle)) {
	for n := 1; ; n++ {
		c := w.cycle(ctx, n)
		if ctx.Err() != nil {
			return // interrupted mid-scan: the partial result is meaningless
		}
		on(c)
		if !w.wait(ctx) {
			return
		}
	}
}

// wait blocks until the next scan is due and reports false when ctx ends.
func (w *Watcher) wait(ctx context.Context) bool {
	timer := time.NewTimer(w.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-w.trigger:
			return true
		case <-timer.C:
			if !w.Paused() {
				return true
			}
			timer.Reset(w.interval)
		}
	}
}

func (w *Watcher) cycle(ctx context.Context, n int) Cycle {
	rep, err := w.scan(ctx)
	if err != nil {
		return Cycle{N: n, Err: err}
	}
	c := Cycle{N: n, Report: rep, Entry: history.Summarize(rep)}
	if w.prev != nil {
		c.Changes = history.Transitions(*w.prev, c.Entry)
	}
	w.prev = &c.Entry
	return c
}
