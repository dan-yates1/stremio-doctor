package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/history"
	"github.com/dan-yates1/stremio-doctor/internal/report"
)

func TestShortDuration(t *testing.T) {
	cases := map[time.Duration]string{
		time.Minute:                       "1m",
		15 * time.Minute:                  "15m",
		time.Hour:                         "1h",
		90 * time.Minute:                  "1h30m",
		time.Minute + 30*time.Second:      "1m30s",
		2*time.Hour + 5*time.Minute + 1e9: "2h5m1s",
	}
	for d, want := range cases {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDescribeChange(t *testing.T) {
	cases := []struct {
		ch   history.Change
		want string
	}{
		{history.Change{Name: "A", From: diagnose.OK, To: diagnose.Fail}, "A: ok → fail"},
		{history.Change{Name: "B", To: diagnose.Warn, Added: true}, "B: new addon (warn)"},
		{history.Change{Name: "C", From: diagnose.OK, Removed: true}, "C: removed"},
	}
	for _, c := range cases {
		if got := describeChange(c.ch, report.Palette{}); !strings.Contains(got, c.want) {
			t.Errorf("describeChange(%+v) = %q, want %q", c.ch, got, c.want)
		}
	}
}
