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
