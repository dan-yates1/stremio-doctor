package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/tray"
	"github.com/dan-yates1/stremio-doctor/internal/watch"
)

// runTray watches from a system tray icon. Each scan rewrites the HTML
// report, which the tray menu opens on request rather than automatically.
func runTray(ctx context.Context, cfg config, out io.Writer, p report.Palette) int {
	if launchedFromExplorer() {
		detachConsole()
		out = io.Discard
	}
	htmlPath := cfg.htmlPath
	if htmlPath == "" {
		htmlPath = filepath.Join(os.TempDir(), defaultHTMLName)
	}
	var (
		mu         sync.Mutex
		reportPath string
	)
	fmt.Fprintf(out, "Watching your addons from the system tray: rescanning every %s.\n", shortDuration(cfg.interval))
	tray.Run(ctx, tray.Options{
		Interval: cfg.interval,
		Scan:     func(ctx context.Context) (report.Report, error) { return scanOnce(ctx, cfg, out, false) },
		Prev:     lastEntry(cfg),
		OnCycle: func(c watch.Cycle) {
			stamp := time.Now().Format("15:04")
			if c.Err != nil {
				fmt.Fprintf(out, "%s  %sscan failed: %v%s\n", stamp, p.Red, c.Err, p.Reset)
				return
			}
			rep := c.Report
			rep.History = recordHistory(cfg, c.Entry)
			printSummary(out, stamp, rep.Counts, c.Changes, p)
			if cfg.jsonPath != "" && cfg.jsonPath != "-" {
				if err := writeJSON(cfg.jsonPath, rep); err != nil {
					fmt.Fprintln(out, "Couldn't write JSON report:", err)
				}
			}
			path, err := writeHTML(htmlPath, rep)
			if err != nil {
				fmt.Fprintln(out, "Couldn't write HTML report:", err)
				return
			}
			mu.Lock()
			reportPath = path
			mu.Unlock()
		},
		OpenReport: func() {
			mu.Lock()
			path := reportPath
			mu.Unlock()
			if path != "" {
				openBrowser(path)
			}
		},
	})
	return 0
}
