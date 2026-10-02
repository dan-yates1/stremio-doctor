package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/probe"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

//go:embed report.html.tmpl
var htmlTemplate string

// minScale keeps bars for a set of fast addons from being stretched full width.
const minScale = time.Second

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"dur":       diagnose.Dur,
	"breakdown": diagnose.Breakdown,
	"upper":     func(s diagnose.Severity) string { return strings.ToUpper(s.String()) },
	"pct": func(d, scale time.Duration) string {
		if scale <= 0 {
			return "0"
		}
		return fmt.Sprintf("%.2f", 100*float64(d)/float64(scale))
	},
	"ok":   func(c scan.Check) int { return len(c.Results) - c.Failures() },
	"cell": func(a Addon, kind string) string { return strings.TrimSpace(cell(a.AddonResult, kind, Palette{})) },
	"items": func(c scan.Check) string {
		if n := c.MaxItems(); n >= 0 {
			return fmt.Sprint(n)
		}
		return "-"
	},
	"lastErr": func(c scan.Check) string {
		if r, ok := c.LastError(); ok {
			if r.Err != "" {
				return r.Err
			}
			return fmt.Sprintf("HTTP %d", r.Status)
		}
		return ""
	},
	"timing":  func(c scan.Check) probe.Timing { return c.Median() },
	"columns": func() []string { return columns },
}).Parse(htmlTemplate))

type htmlData struct {
	Report
	Scale    time.Duration
	AllFinds []diagnose.Finding
}

// WriteHTML renders the self-contained HTML report.
func WriteHTML(w io.Writer, r Report) error {
	all := append(append([]diagnose.Finding(nil), r.Findings...), addonFindings(r.Addons)...)
	diagnose.Sort(all)
	return tmpl.Execute(w, htmlData{Report: r, Scale: scale(r), AllFinds: all})
}

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func scale(r Report) time.Duration {
	s := minScale
	for _, a := range r.Addons {
		for _, c := range a.Checks {
			if t := c.Median().Total; t > s {
				s = t
			}
		}
	}
	return s
}
