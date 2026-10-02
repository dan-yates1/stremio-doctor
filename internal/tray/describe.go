// Package tray shows watch-mode results as a system tray icon.
package tray

import (
	"fmt"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/watch"
)

// State picks the tray icon.
type State int

const (
	StateScanning State = iota
	StateOK
	StateWarn
	StateFail
)

// maxWorst is how many problem addons the menu lists.
const maxWorst = 3

// Status is what the tray shows after a scan.
type Status struct {
	Summary string   // first menu line and tooltip
	Worst   []string // problem addons, worst first
	Icon    State
}

// Describe turns a scan into tray text. It is separate from the UI so it
// can be tested.
func Describe(c watch.Cycle, at time.Time) Status {
	stamp := at.Format("15:04")
	if c.Err != nil {
		return Status{Summary: fmt.Sprintf("Last scan failed (%s): %s", stamp, truncate(c.Err.Error(), 60)), Icon: StateWarn}
	}
	n := c.Report.Counts
	s := Status{Summary: fmt.Sprintf("%d OK · %d warning · %d failing (%s)", n.OK+n.Info, n.Warn, n.Fail, stamp)}
	switch {
	case n.Fail > 0:
		s.Icon = StateFail
	case n.Warn > 0:
		s.Icon = StateWarn
	default:
		s.Icon = StateOK
	}
	// Report.Addons is already sorted worst first.
	for _, a := range c.Report.Addons {
		if a.Status < diagnose.Warn || len(s.Worst) == maxWorst {
			break
		}
		line := a.Name
		if len(a.Findings) > 0 {
			line += ": " + a.Findings[0].Title
		}
		s.Worst = append(s.Worst, truncate(line, 60))
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
