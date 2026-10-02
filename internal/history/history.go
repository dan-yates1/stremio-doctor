// Package history keeps a compact record of past scans so watch mode can
// show trends and status changes. Entries never contain addon URLs.
package history

import (
	"sort"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

// Entry summarises one scan.
type Entry struct {
	Time     time.Time         `json:"time"`
	Version  string            `json:"version"`
	Source   string            `json:"source"`
	Counts   report.Counts     `json:"counts"`
	Baseline []BaselineSummary `json:"baseline"`
	Addons   []AddonSummary    `json:"addons"`
}

// BaselineSummary is one reference service's result.
type BaselineSummary struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Ms   int64  `json:"ms"`
}

// AddonSummary is one addon's result, without its URL.
type AddonSummary struct {
	Key    string            `json:"key"` // manifest id + host, stable across runs
	Name   string            `json:"name"`
	Host   string            `json:"host"`
	Status diagnose.Severity `json:"status"`
	Checks []CheckSummary    `json:"checks"`
}

// CheckSummary is one request type's result.
type CheckSummary struct {
	Kind     string `json:"kind"`
	MedianMs int64  `json:"medianMs"`
	Failures int    `json:"failures"`
	Runs     int    `json:"runs"`
	ErrKind  string `json:"errorKind,omitempty"`
}

// Summarize reduces a report to a history entry.
func Summarize(rep report.Report) Entry {
	e := Entry{Time: rep.Generated, Version: rep.Version, Source: rep.Source, Counts: rep.Counts}
	for _, b := range rep.Baseline {
		e.Baseline = append(e.Baseline, BaselineSummary{Name: b.Name, OK: b.Reachable(), Ms: b.Result.Timing.Total.Milliseconds()})
	}
	for _, a := range rep.Addons {
		s := AddonSummary{Key: key(a.AddonResult), Name: a.Name, Host: a.Host, Status: a.Status}
		for _, c := range a.Checks {
			cs := CheckSummary{Kind: c.Kind, MedianMs: c.Median().Total.Milliseconds(), Failures: c.Failures(), Runs: len(c.Results)}
			if r, ok := c.LastError(); ok {
				cs.ErrKind = r.ErrKind
			}
			s.Checks = append(s.Checks, cs)
		}
		e.Addons = append(e.Addons, s)
	}
	return e
}

func key(a scan.AddonResult) string {
	id := a.ID
	if id == "" {
		id = a.Name
	}
	return id + "@" + a.Host
}

// Change is an addon whose status differs from the previous scan.
type Change struct {
	Name     string
	From, To diagnose.Severity
	Added    bool // new since the previous scan; From is meaningless
	Removed  bool // gone since the previous scan; To is meaningless
}

// Transitions lists status changes between two consecutive scans.
func Transitions(prev, cur Entry) []Change {
	before := map[string]AddonSummary{}
	for _, a := range prev.Addons {
		before[a.Key] = a
	}
	var out []Change
	for _, a := range cur.Addons {
		p, ok := before[a.Key]
		delete(before, a.Key)
		switch {
		case !ok:
			out = append(out, Change{Name: a.Name, To: a.Status, Added: true})
		case p.Status != a.Status:
			out = append(out, Change{Name: a.Name, From: p.Status, To: a.Status})
		}
	}
	for _, a := range prev.Addons {
		if _, gone := before[a.Key]; gone {
			out = append(out, Change{Name: a.Name, From: a.Status, Removed: true})
		}
	}
	return out
}

// View builds the report's history section for the addons in the newest
// entry, showing at most maxCells recent scans. It returns nil with fewer
// than two entries, since one scan is no history.
func View(entries []Entry, maxCells int) *report.HistoryView {
	if len(entries) < 2 {
		return nil
	}
	latest := entries[len(entries)-1]
	recent := entries
	if len(recent) > maxCells {
		recent = recent[len(recent)-maxCells:]
	}
	v := &report.HistoryView{From: entries[0].Time, Scans: len(entries)}
	for _, a := range latest.Addons {
		row := report.HistoryRow{Name: a.Name, Host: a.Host}
		up, seen, streams := 0, 0, []int64(nil)
		for _, e := range entries {
			s, ok := find(e, a.Key)
			if !ok {
				continue
			}
			seen++
			if s.Status != diagnose.Fail {
				up++
			}
			streams = append(streams, streamMs(s)...)
		}
		row.Uptime = 100 * float64(up) / float64(seen)
		row.StreamMs = medianMs(streams)
		for _, e := range recent {
			s, ok := find(e, a.Key)
			row.Cells = append(row.Cells, report.HistoryCell{Time: e.Time, Present: ok, Status: s.Status})
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

func find(e Entry, key string) (AddonSummary, bool) {
	for _, a := range e.Addons {
		if a.Key == key {
			return a, true
		}
	}
	return AddonSummary{}, false
}

// streamMs returns the median times of stream checks that succeeded at least once.
func streamMs(a AddonSummary) []int64 {
	var out []int64
	for _, c := range a.Checks {
		if c.Kind == scan.KindStream && c.Failures < c.Runs {
			out = append(out, c.MedianMs)
		}
	}
	return out
}

func medianMs(ms []int64) int64 {
	if len(ms) == 0 {
		return 0
	}
	s := append([]int64(nil), ms...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}
