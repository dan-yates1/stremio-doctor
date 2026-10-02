package watch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

// fakeScan returns each status in turn for a single addon, or an error for -1.
func fakeScan(statuses ...diagnose.Severity) ScanFunc {
	i := 0
	return func(context.Context) (report.Report, error) {
		s := statuses[i%len(statuses)]
		i++
		if s < 0 {
			return report.Report{}, errors.New("boom")
		}
		return report.Report{Generated: time.Now(), Addons: []report.Addon{
			{AddonResult: scan.AddonResult{Name: "A", ID: "a", Host: "h"}, Status: s},
		}}, nil
	}
}

// collect runs the watcher until it has seen n cycles.
func collect(t *testing.T, w *Watcher, n int, during func()) []Cycle {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []Cycle
	done := make(chan struct{})
	go func() {
		w.Run(ctx, func(c Cycle) {
			got = append(got, c)
			if len(got) == 1 && during != nil {
				during()
			}
			if len(got) == n {
				cancel()
			}
		})
		close(done)
	}()
	<-done
	if len(got) != n {
		t.Fatalf("got %d cycles, want %d", len(got), n)
	}
	return got
}

func TestRunReportsChangesAndSurvivesErrors(t *testing.T) {
	w := New(time.Millisecond, fakeScan(diagnose.OK, -1, diagnose.Fail), nil)
	got := collect(t, w, 3, nil)
	if got[0].N != 1 || got[0].Err != nil || len(got[0].Changes) != 0 {
		t.Errorf("first cycle: %+v", got[0])
	}
	if got[1].Err == nil {
		t.Error("second cycle should carry the scan error")
	}
	// The change is measured against the last successful scan.
	if ch := got[2].Changes; len(ch) != 1 || ch[0].From != diagnose.OK || ch[0].To != diagnose.Fail {
		t.Errorf("third cycle changes: %+v", ch)
	}
}

func TestScanNowWorksWhilePaused(t *testing.T) {
	w := New(time.Hour, fakeScan(diagnose.OK), nil)
	w.SetPaused(true)
	// The first scan always runs; ScanNow forces the second despite the
	// hour-long interval and the pause.
	got := collect(t, w, 2, w.ScanNow)
	if got[1].N != 2 {
		t.Errorf("second cycle N = %d", got[1].N)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		New(time.Millisecond, fakeScan(diagnose.OK), nil).Run(ctx, func(Cycle) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
