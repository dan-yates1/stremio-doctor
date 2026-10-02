package report

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

const (
	nameWidth = 24
	hostWidth = 26
	cellWidth = 10
	wrapWidth = 96
)

var columns = []string{scan.KindManifest, scan.KindCatalog, scan.KindMeta, scan.KindStream, scan.KindSubtitles}

// Palette is a set of ANSI codes; the zero value prints no colour.
type Palette struct{ Red, Yellow, Cyan, Green, Dim, Bold, Reset string }

// ANSI is the colour palette for terminals that support it.
var ANSI = Palette{Red: "\x1b[31m", Yellow: "\x1b[33m", Cyan: "\x1b[36m", Green: "\x1b[32m",
	Dim: "\x1b[2m", Bold: "\x1b[1m", Reset: "\x1b[0m"}

// WriteTerminal prints a compact summary table and the findings.
func WriteTerminal(w io.Writer, r Report, p Palette) {
	fmt.Fprintf(w, "%sstremio-doctor %s%s: %d addons, %d rounds each\n", p.Bold, r.Version, p.Reset, len(r.Addons), r.Rounds)
	fmt.Fprintf(w, "%sAddon list from: %s%s\n", p.Dim, r.Source, p.Reset)
	for _, n := range r.Notes {
		fmt.Fprintf(w, "%snote: %s%s\n", p.Yellow, n, p.Reset)
	}

	fmt.Fprintf(w, "\n%sBaseline%s\n", p.Bold, p.Reset)
	for _, b := range r.Baseline {
		if b.Reachable() {
			fmt.Fprintf(w, "  %s✓%s %-28s %s\n", p.Green, p.Reset, b.Name, diagnose.Dur(b.Result.Timing.Total))
		} else {
			fmt.Fprintf(w, "  %s✗%s %-28s %s\n", p.Red, p.Reset, b.Name, b.Note)
		}
	}

	fmt.Fprintf(w, "\n%s  %s %s", p.Bold, pad("Addon", nameWidth), pad("Host", hostWidth))
	for _, c := range columns {
		fmt.Fprintf(w, " %s", padLeft(c, cellWidth))
	}
	fmt.Fprintf(w, " %s%s\n", padLeft("streams", 8), p.Reset)
	for _, a := range r.Addons {
		icon, col := statusIcon(a.Status, p)
		fmt.Fprintf(w, "%s%s%s %s %s", col, icon, p.Reset, pad(a.Name, nameWidth), pad(a.Host, hostWidth))
		for _, kind := range columns {
			fmt.Fprintf(w, " %s", cell(a.AddonResult, kind, p))
		}
		fmt.Fprintf(w, " %s\n", padLeft(streamCount(a.AddonResult), 8))
	}

	all := append(append([]diagnose.Finding(nil), r.Findings...), addonFindings(r.Addons)...)
	diagnose.Sort(all)
	fmt.Fprintf(w, "\n%sFindings%s\n", p.Bold, p.Reset)
	if len(all) == 0 {
		fmt.Fprintf(w, "  %sNo problems found. All addons answered quickly.%s\n", p.Green, p.Reset)
	}
	for _, f := range all {
		_, col := statusIcon(f.Severity, p)
		who := ""
		if f.Addon != "" {
			who = f.Addon + ": "
		}
		fmt.Fprintf(w, "  %s[%s]%s %s%s\n", col, strings.ToUpper(f.Severity.String()), p.Reset, who, f.Title)
		for _, line := range wrap(f.Detail, wrapWidth) {
			fmt.Fprintf(w, "         %s%s%s\n", p.Dim, line, p.Reset)
		}
	}
	fmt.Fprintf(w, "\nSummary: %s%d failing%s, %s%d warnings%s, %d ok/info\n",
		p.Red, r.Counts.Fail, p.Reset, p.Yellow, r.Counts.Warn, p.Reset, r.Counts.OK+r.Counts.Info)
}

func addonFindings(as []Addon) []diagnose.Finding {
	var fs []diagnose.Finding
	for _, a := range as {
		fs = append(fs, a.Findings...)
	}
	return fs
}

// cell shows the median time for the slowest check of a kind, or FAIL / -.
func cell(a scan.AddonResult, kind string, p Palette) string {
	var worst time.Duration
	found, failed := false, false
	for _, c := range a.Checks {
		if c.Kind != kind {
			continue
		}
		found = true
		if c.AllFailed() {
			failed = true
			continue
		}
		if t := c.Median().Total; t > worst {
			worst = t
		}
	}
	switch {
	case !found:
		return p.Dim + padLeft("-", cellWidth) + p.Reset
	case failed:
		return p.Red + padLeft("FAIL", cellWidth) + p.Reset
	}
	col := ""
	if lim, ok := diagnose.Thresholds[kind]; ok {
		switch {
		case worst >= lim.VerySlow:
			col = p.Red
		case worst >= lim.Slow:
			col = p.Yellow
		}
	}
	return col + padLeft(diagnose.Dur(worst), cellWidth) + p.Reset
}

func streamCount(a scan.AddonResult) string {
	best := -1
	for _, c := range a.Checks {
		if c.Kind == scan.KindStream && c.MaxItems() > best {
			best = c.MaxItems()
		}
	}
	if best < 0 {
		return "-"
	}
	return fmt.Sprint(best)
}

func statusIcon(s diagnose.Severity, p Palette) (string, string) {
	switch s {
	case diagnose.Fail:
		return "✗", p.Red
	case diagnose.Warn:
		return "!", p.Yellow
	case diagnose.Info:
		return "i", p.Cyan
	}
	return "✓", p.Green
}

func pad(s string, n int) string {
	if utf8.RuneCountInString(s) > n {
		return string([]rune(s)[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-utf8.RuneCountInString(s))
}

func padLeft(s string, n int) string {
	if c := utf8.RuneCountInString(s); c < n {
		return strings.Repeat(" ", n-c) + s
	}
	return s
}

func wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
