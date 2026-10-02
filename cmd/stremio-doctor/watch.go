package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/history"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/watch"
)

const (
	defaultInterval = 15 * time.Minute
	minInterval     = time.Minute
	// historyCells is how many recent scans the report's history strip shows.
	historyCells = 96
)

// runWatch rescans until interrupted, printing a line per scan and the
// status changes since the previous one.
func runWatch(ctx context.Context, cfg config, out io.Writer, p report.Palette) int {
	first := true
	w := watch.New(cfg.interval, func(ctx context.Context) (report.Report, error) {
		verbose := first
		first = false
		return scanOnce(ctx, cfg, out, verbose)
	}, lastEntry(cfg))

	fmt.Fprintf(out, "Watching your addons: rescanning every %s. Press Ctrl+C to stop.\n", shortDuration(cfg.interval))
	w.Run(ctx, func(c watch.Cycle) { printCycle(out, cfg, p, c) })
	fmt.Fprintln(out, "\nStopped watching.")
	return 0
}

func printCycle(out io.Writer, cfg config, p report.Palette, c watch.Cycle) {
	stamp := time.Now().Format("15:04")
	if c.Err != nil {
		fmt.Fprintf(out, "%s  %sscan failed: %v%s\n", stamp, p.Red, c.Err, p.Reset)
		return
	}
	rep := c.Report
	rep.History = recordHistory(cfg, c.Entry)
	if c.N == 1 {
		report.WriteTerminal(out, rep, p)
		fmt.Fprintln(out)
	}
	n := rep.Counts
	fmt.Fprintf(out, "%s  %s%d ok%s · %s%d warn%s · %s%d fail%s\n", stamp,
		p.Green, n.OK+n.Info, p.Reset, p.Yellow, n.Warn, p.Reset, p.Red, n.Fail, p.Reset)
	for _, ch := range c.Changes {
		fmt.Fprintf(out, "       %s\n", describeChange(ch, p))
	}
	writeFiles(out, cfg, rep, c.N == 1)
}

func describeChange(ch history.Change, p report.Palette) string {
	switch {
	case ch.Removed:
		return fmt.Sprintf("%s%s: removed%s", p.Dim, ch.Name, p.Reset)
	case ch.Added:
		return fmt.Sprintf("%s%s: new addon (%s)%s", severityColor(ch.To, p), ch.Name, ch.To, p.Reset)
	}
	return fmt.Sprintf("%s%s: %s → %s%s", severityColor(ch.To, p), ch.Name, ch.From, ch.To, p.Reset)
}

func severityColor(s diagnose.Severity, p report.Palette) string {
	switch s {
	case diagnose.Fail:
		return p.Red
	case diagnose.Warn:
		return p.Yellow
	}
	return p.Green
}

// recordHistory appends the scan to the history file, drops expired
// entries, and returns the report's history section. Problems are reported
// but never fatal: history is a nice-to-have.
func recordHistory(cfg config, e history.Entry) *report.HistoryView {
	if cfg.historyPath == "" {
		return nil
	}
	if err := history.Append(cfg.historyPath, e); err != nil {
		fmt.Fprintln(os.Stderr, "Couldn't save history:", err)
		return nil
	}
	entries, err := history.Prune(cfg.historyPath, e.Time.Add(-keep(cfg)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Couldn't tidy history:", err)
	}
	return history.View(entries, historyCells)
}

// lastEntry is the most recent saved scan, so watch mode can report changes
// that happened while it wasn't running.
func lastEntry(cfg config) *history.Entry {
	if cfg.historyPath == "" {
		return nil
	}
	entries, err := history.Load(cfg.historyPath, time.Now().Add(-keep(cfg)))
	if err != nil || len(entries) == 0 {
		return nil
	}
	return &entries[len(entries)-1]
}

// shortDuration drops zero units: 15m0s → 15m, 1h0m0s → 1h.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func keep(cfg config) time.Duration { return time.Duration(cfg.keepDays) * 24 * time.Hour }
