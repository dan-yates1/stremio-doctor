// Package report assembles scan results and findings into a Report and
// renders it as terminal text, HTML or JSON.
package report

import (
	"sort"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

// Report is the complete output of one run.
type Report struct {
	Version   string             `json:"version"`
	Generated time.Time          `json:"generated"`
	Source    string             `json:"source"`
	Notes     []string           `json:"notes,omitempty"`
	Rounds    int                `json:"rounds"`
	Baseline  []scan.Baseline    `json:"baseline"`
	Addons    []Addon            `json:"addons"`
	Findings  []diagnose.Finding `json:"findings"` // across several addons
	Counts    Counts             `json:"counts"`
	Redacted  bool               `json:"redacted"`
	History   *HistoryView       `json:"history,omitempty"` // set by the caller from past runs
}

// HistoryView is the past-runs section of a report: one row per current
// addon, oldest scan first. It is built by package history.
type HistoryView struct {
	From  time.Time    `json:"from"`
	Scans int          `json:"scans"`
	Rows  []HistoryRow `json:"rows"`
}

// HistoryRow is one addon's record over the history window.
type HistoryRow struct {
	Name     string        `json:"name"`
	Host     string        `json:"host"`
	Uptime   float64       `json:"uptime"`   // percent of scans where it wasn't failing
	StreamMs int64         `json:"streamMs"` // median stream time, 0 when unknown
	Cells    []HistoryCell `json:"cells"`
}

// HistoryCell is the addon's status in one past scan.
type HistoryCell struct {
	Time    time.Time         `json:"time"`
	Present bool              `json:"present"` // false when the addon wasn't installed then
	Status  diagnose.Severity `json:"status"`
}

// Addon is one addon's measurements plus its diagnosis.
type Addon struct {
	scan.AddonResult
	Status   diagnose.Severity  `json:"status"`
	Findings []diagnose.Finding `json:"findings"`
}

// Counts is how many addons ended up at each status.
type Counts struct {
	OK   int `json:"ok"`
	Info int `json:"info"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

// Input is everything Build needs.
type Input struct {
	Version  string
	Source   string
	Notes    []string
	Rounds   int
	Baseline []scan.Baseline
	Results  []scan.AddonResult
	ShowURLs bool // false redacts addon URLs (they usually contain secrets)
}

// Build diagnoses results and assembles the report, worst addons first.
func Build(in Input) Report {
	r := Report{
		Version: in.Version, Generated: time.Now(), Source: in.Source, Notes: in.Notes,
		Rounds: in.Rounds, Baseline: in.Baseline, Redacted: !in.ShowURLs,
		Findings: diagnose.Global(in.Results),
	}
	diagnose.Sort(r.Findings)
	for _, res := range in.Results {
		fs := diagnose.Addon(res)
		diagnose.Sort(fs)
		a := Addon{AddonResult: res, Status: diagnose.Status(fs), Findings: fs}
		if !in.ShowURLs {
			a.AddonResult = redactResult(res)
		}
		r.Addons = append(r.Addons, a)
		switch a.Status {
		case diagnose.Fail:
			r.Counts.Fail++
		case diagnose.Warn:
			r.Counts.Warn++
		case diagnose.Info:
			r.Counts.Info++
		default:
			r.Counts.OK++
		}
	}
	sort.SliceStable(r.Addons, func(i, j int) bool { return r.Addons[i].Status > r.Addons[j].Status })
	return r
}

func redactResult(res scan.AddonResult) scan.AddonResult {
	res.TransportURL = Redact(res.TransportURL)
	checks := make([]scan.Check, len(res.Checks))
	for i, c := range res.Checks {
		c.URL = Redact(c.URL)
		checks[i] = c
	}
	res.Checks = checks
	return res
}
