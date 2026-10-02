package tray

import (
	"context"
	"time"

	"fyne.io/systray"

	"github.com/dan-yates1/stremio-doctor/internal/history"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/watch"
)

// Options configure the tray.
type Options struct {
	Interval   time.Duration
	Scan       watch.ScanFunc
	Prev       *history.Entry    // last saved scan, for change detection
	OnCycle    func(watch.Cycle) // saves history and reports; runs before the tray updates
	OpenReport func()            // opens the latest HTML report
}

// Run shows the tray icon and watches until the user quits or ctx ends. It
// must be called from the main goroutine (a macOS requirement).
func Run(ctx context.Context, opts Options) {
	ctx, cancel := context.WithCancel(ctx)
	systray.Run(func() { onReady(ctx, cancel, opts) }, cancel)
}

type menu struct {
	status *systray.MenuItem
	worst  [maxWorst]*systray.MenuItem
	scan   *systray.MenuItem
	open   *systray.MenuItem
	pause  *systray.MenuItem
	quit   *systray.MenuItem
}

func onReady(ctx context.Context, cancel context.CancelFunc, opts Options) {
	systray.SetIcon(iconBytes(StateScanning))
	systray.SetTitle("")
	systray.SetTooltip("Stremio Doctor: scanning your addons…")

	m := menu{status: systray.AddMenuItem("Scanning your addons…", "")}
	m.status.Disable()
	for i := range m.worst {
		m.worst[i] = systray.AddMenuItem("", "")
		m.worst[i].Disable()
		m.worst[i].Hide()
	}
	systray.AddSeparator()
	m.scan = systray.AddMenuItem("Scan now", "Test every addon again now")
	m.open = systray.AddMenuItem("Open report", "Open the latest report in your browser")
	m.open.Disable()
	m.pause = systray.AddMenuItemCheckbox("Pause watching", "Stop the regular scans", false)
	systray.AddSeparator()
	m.quit = systray.AddMenuItem("Quit", "Stop watching and close Stremio Doctor")

	scanning := func(ctx context.Context) (report.Report, error) {
		systray.SetIcon(iconBytes(StateScanning))
		m.status.SetTitle("Scanning your addons…")
		return opts.Scan(ctx)
	}
	w := watch.New(opts.Interval, scanning, opts.Prev)
	go func() {
		w.Run(ctx, func(c watch.Cycle) {
			opts.OnCycle(c)
			m.show(Describe(c, time.Now()))
		})
		systray.Quit()
	}()
	go m.handleClicks(ctx, cancel, w, opts.OpenReport)
}

func (m menu) show(s Status) {
	systray.SetIcon(iconBytes(s.Icon))
	systray.SetTooltip("Stremio Doctor: " + s.Summary)
	m.status.SetTitle(s.Summary)
	for i, item := range m.worst {
		if i < len(s.Worst) {
			item.SetTitle(s.Worst[i])
			item.Show()
		} else {
			item.Hide()
		}
	}
	m.open.Enable()
}

func (m menu) handleClicks(ctx context.Context, cancel context.CancelFunc, w *watch.Watcher, open func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.scan.ClickedCh:
			w.ScanNow()
		case <-m.open.ClickedCh:
			open()
		case <-m.pause.ClickedCh:
			if m.pause.Checked() {
				m.pause.Uncheck()
			} else {
				m.pause.Check()
			}
			w.SetPaused(m.pause.Checked())
		case <-m.quit.ClickedCh:
			cancel() // the watch goroutine then calls systray.Quit
			return
		}
	}
}
