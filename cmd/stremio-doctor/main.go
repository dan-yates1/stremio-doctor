// Command stremio-doctor finds your installed Stremio addons, tests them
// from your own network, and explains which ones are slow or broken and why.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/discover"
	"github.com/dan-yates1/stremio-doctor/internal/history"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const defaultHTMLName = "stremio-doctor-report.html"

type config struct {
	discover discover.Options
	scan     scan.Options
	htmlPath string
	jsonPath string
	noOpen   bool
	showURLs bool
	noColor  bool

	watch       bool
	tray        bool
	interval    time.Duration
	historyPath string
	keepDays    int
}

type urlList []string

func (u *urlList) String() string     { return strings.Join(*u, ",") }
func (u *urlList) Set(v string) error { *u = append(*u, v); return nil }

func main() {
	cfg, showVersion := parseFlags()
	if showVersion {
		fmt.Println("stremio-doctor", version)
		return
	}
	code := run(cfg)
	// After Ctrl+C in watch mode the user is done; don't ask for Enter too.
	if launchedFromExplorer() && !cfg.watch {
		fmt.Print("\nPress Enter to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
	}
	os.Exit(code)
}

func parseFlags() (config, bool) {
	var (
		cfg     config
		manual  urlList
		version bool
	)
	flag.Var(&manual, "addon", "test this addon manifest URL instead of your installed addons (repeatable)")
	flag.StringVar(&cfg.discover.AddonsFile, "addons-file", "", "file with one addon manifest URL per line")
	flag.StringVar(&cfg.discover.AuthKey, "auth-key", os.Getenv("STREMIO_AUTH_KEY"), "Stremio auth key (default: read from the Stremio app, or $STREMIO_AUTH_KEY)")
	flag.StringVar(&cfg.discover.StorageDir, "profile-dir", "", "Stremio 'Local Storage/leveldb' folder, if auto-detection misses it")
	flag.IntVar(&cfg.scan.Rounds, "rounds", scan.DefaultOptions.Rounds, "how many times to repeat each request")
	flag.DurationVar(&cfg.scan.Timeout, "timeout", scan.DefaultOptions.Timeout, "per-request timeout")
	flag.IntVar(&cfg.scan.Concurrency, "concurrency", scan.DefaultOptions.Concurrency, "addons tested at the same time")
	flag.StringVar(&cfg.htmlPath, "html", defaultHTMLName, "where to write the HTML report (empty to skip)")
	flag.StringVar(&cfg.jsonPath, "json", "", "also write a JSON report to this file ('-' for stdout)")
	flag.BoolVar(&cfg.noOpen, "no-open", false, "don't open the HTML report in the browser")
	flag.BoolVar(&cfg.showURLs, "show-urls", false, "show full addon URLs in reports (they may contain API keys!)")
	flag.BoolVar(&cfg.noColor, "no-color", false, "disable coloured output")
	flag.BoolVar(&cfg.watch, "watch", false, "keep running and rescan every --interval")
	flag.DurationVar(&cfg.interval, "interval", defaultInterval, "time between scans in watch mode (minimum 1m)")
	flag.StringVar(&cfg.historyPath, "history", history.DefaultPath(), "file that keeps a summary of every scan (empty to disable)")
	flag.IntVar(&cfg.keepDays, "keep-days", 30, "days of history to keep")
	flag.BoolVar(&cfg.tray, "tray", false, "show a system tray icon and keep watching (implies --watch)")
	flag.BoolVar(&version, "version", false, "print version and exit")
	flag.Parse()
	cfg.discover.ManualURLs = manual
	cfg.watch = cfg.watch || cfg.tray
	return cfg, version
}

func run(cfg config) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// With --json - the JSON owns stdout, so human output goes to stderr.
	var out io.Writer = os.Stdout
	if cfg.jsonPath == "-" {
		out = os.Stderr
	}
	palette := report.Palette{}
	if !cfg.noColor && os.Getenv("NO_COLOR") == "" && isTerminal(out) && enableColor() {
		palette = report.ANSI
	}

	if cfg.watch {
		if cfg.interval < minInterval {
			fmt.Fprintf(os.Stderr, "--interval must be at least %s, so addons aren't hammered.\n", minInterval)
			return 2
		}
		if cfg.tray {
			return runTray(ctx, cfg, out, palette)
		}
		return runWatch(ctx, cfg, out, palette)
	}

	rep, err := scanOnce(ctx, cfg, out, true)
	var de *discoveryError
	switch {
	case errors.As(err, &de):
		printDiscoveryHelp(out, de.err, de.notes)
		return 1
	case err != nil:
		fmt.Fprintln(out, "Interrupted.")
		return 130
	}
	rep.History = recordHistory(cfg, history.Summarize(rep))
	report.WriteTerminal(out, rep, palette)
	return writeFiles(out, cfg, rep, true)
}

// discoveryError is a failure to find any addons, with the notes gathered
// while looking.
type discoveryError struct {
	err   error
	notes []string
}

func (e *discoveryError) Error() string { return e.err.Error() }
func (e *discoveryError) Unwrap() error { return e.err }

// scanOnce discovers and scans the addons and builds the report. verbose
// prints progress as it goes. It returns ctx's error when interrupted.
func scanOnce(ctx context.Context, cfg config, out io.Writer, verbose bool) (report.Report, error) {
	logf := func(format string, a ...any) {
		if verbose {
			fmt.Fprintf(out, format, a...)
		}
	}
	logf("Looking for your Stremio addons...\n")
	found, err := discover.Discover(ctx, cfg.discover)
	if err != nil {
		return report.Report{}, &discoveryError{err: err, notes: found.Notes}
	}

	logf("Found %d addons. Testing each one %d times...\n", len(found.Addons), cfg.scan.Rounds)
	var (
		baseline []scan.Baseline
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		baseline = scan.RunBaseline(ctx, scan.BaselineTargets, 10*time.Second)
	}()
	var progress func(done, total int)
	if verbose && isTerminal(out) {
		progress = func(done, total int) { fmt.Fprintf(out, "\r  %d/%d addons tested", done, total) }
	}
	results := scan.Run(ctx, found.Addons, cfg.scan, progress)
	wg.Wait()
	if progress != nil {
		fmt.Fprint(out, "\r"+strings.Repeat(" ", 40)+"\r")
	}
	if err := ctx.Err(); err != nil {
		return report.Report{}, err
	}

	return report.Build(report.Input{
		Version: version, Source: found.Source, Notes: found.Notes, Rounds: cfg.scan.Rounds,
		Baseline: baseline, Results: results, ShowURLs: cfg.showURLs,
	}), nil
}

// writeFiles writes the JSON and HTML reports. announce prints the HTML path
// and opens it (watch mode only does that for the first scan).
func writeFiles(out io.Writer, cfg config, rep report.Report, announce bool) int {
	code := 0
	if cfg.jsonPath != "" {
		if err := writeJSON(cfg.jsonPath, rep); err != nil {
			fmt.Fprintln(os.Stderr, "Couldn't write JSON report:", err)
			code = 1
		}
	}
	if cfg.htmlPath == "" {
		return code
	}
	path, err := writeHTML(cfg.htmlPath, rep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Couldn't write HTML report:", err)
		return 1
	}
	if !announce {
		return code
	}
	fmt.Fprintf(out, "\nFull report: %s\n", path)
	if !cfg.noOpen {
		if err := openBrowser(path); err != nil {
			fmt.Fprintln(out, "(Couldn't open it automatically. Open the file above in your browser.)")
		}
	}
	return code
}

func writeJSON(path string, rep report.Report) error {
	if path == "-" {
		return report.WriteJSON(os.Stdout, rep)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := report.WriteJSON(f, rep); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeHTML writes the report, falling back to the temp dir when the target
// folder isn't writable (e.g. the exe sits in Program Files).
func writeHTML(path string, rep report.Report) (string, error) {
	try := func(p string) (string, error) {
		f, err := os.Create(p)
		if err != nil {
			return "", err
		}
		if err := report.WriteHTML(f, rep); err != nil {
			f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return filepath.Abs(p)
	}
	p, err := try(path)
	if err == nil || path != defaultHTMLName {
		return p, err
	}
	return try(filepath.Join(os.TempDir(), defaultHTMLName))
}

func printDiscoveryHelp(out io.Writer, err error, notes []string) {
	fmt.Fprintln(out, "\nCouldn't find your addons:", err)
	for _, n := range notes {
		fmt.Fprintln(out, "  -", n)
	}
	if !errors.Is(err, discover.ErrNothingFound) {
		return
	}
	fmt.Fprint(out, `
Other ways to run it:
  * Make sure you're logged in to the Stremio desktop app, then run this again.
  * Pass your auth key: open web.stremio.com, log in, press F12, and in the
    Console run:  JSON.parse(localStorage.getItem("profile")).auth.key
    then:         stremio-doctor --auth-key <that key>
  * Test specific addons: stremio-doctor --addon https://example.com/manifest.json
`)
}

func openBrowser(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
