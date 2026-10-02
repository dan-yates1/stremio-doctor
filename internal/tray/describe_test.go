package tray

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
	"github.com/dan-yates1/stremio-doctor/internal/watch"
)

func addon(name string, s diagnose.Severity, finding string) report.Addon {
	a := report.Addon{AddonResult: scan.AddonResult{Name: name}, Status: s}
	if finding != "" {
		a.Findings = []diagnose.Finding{{Severity: s, Title: finding}}
	}
	return a
}

func TestDescribe(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 4, 0, 0, time.UTC)
	cases := []struct {
		name    string
		cycle   watch.Cycle
		icon    State
		summary string
		worst   []string
	}{
		{"scan error", watch.Cycle{Err: errors.New("no addons found")}, StateWarn,
			"Last scan failed (12:04): no addons found", nil},
		{"all ok", watch.Cycle{Report: report.Report{Counts: report.Counts{OK: 2, Info: 1},
			Addons: []report.Addon{addon("A", diagnose.Info, "note"), addon("B", diagnose.OK, "")}}},
			StateOK, "3 OK · 0 warning · 0 failing (12:04)", nil},
		{"failing first, capped at three", watch.Cycle{Report: report.Report{Counts: report.Counts{Fail: 2, Warn: 2},
			Addons: []report.Addon{
				addon("F1", diagnose.Fail, "Domain doesn't resolve"), addon("F2", diagnose.Fail, ""),
				addon("W1", diagnose.Warn, "Slow stream"), addon("W2", diagnose.Warn, "Slow stream"),
			}}},
			StateFail, "0 OK · 2 warning · 2 failing (12:04)",
			[]string{"F1: Domain doesn't resolve", "F2", "W1: Slow stream"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Describe(c.cycle, at)
			if got.Icon != c.icon || got.Summary != c.summary || strings.Join(got.Worst, "|") != strings.Join(c.worst, "|") {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestIconsEmbedded(t *testing.T) {
	for _, s := range []State{StateScanning, StateOK, StateWarn, StateFail} {
		if len(iconBytes(s)) == 0 {
			t.Errorf("no icon for state %d", s)
		}
	}
}
