package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/probe"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

const secret = "realdebrid=SECRETKEY123"

func sampleReport(showURLs bool) report.Report {
	base := "https://host.example/" + secret
	res := scan.AddonResult{
		Name: "Torrentio", ID: "com.stremio.torrentio", Host: "host.example", TransportURL: base + "/manifest.json",
		Checks: []scan.Check{
			{Kind: scan.KindManifest, URL: base + "/manifest.json",
				Results: []probe.Result{{Status: 200, Timing: probe.Timing{Total: 80 * time.Millisecond}}}, Items: []int{-1}},
			{Kind: scan.KindStream, URL: base + "/stream/movie/tt0111161.json",
				Results: []probe.Result{
					{Status: 200, Timing: probe.Timing{Total: 4 * time.Second}},
					{Err: "timed out", ErrKind: probe.ErrTimeout, Timing: probe.Timing{Total: 15 * time.Second}},
				}, Items: []int{12, -1}},
		},
	}
	return report.Build(report.Input{Version: "test", Source: "test", Rounds: 2,
		Results: []scan.AddonResult{res}, ShowURLs: showURLs})
}

func TestSummarizeNeverStoresURLs(t *testing.T) {
	// Even with --show-urls the history file must not hold addon URLs.
	for _, show := range []bool{false, true} {
		line, err := json.Marshal(Summarize(sampleReport(show)))
		if err != nil {
			t.Fatal(err)
		}
		if s := string(line); strings.Contains(s, "SECRETKEY") || strings.Contains(s, "manifest.json") {
			t.Errorf("showURLs=%v: entry contains a URL: %s", show, s)
		}
	}
}

func TestSummarize(t *testing.T) {
	e := Summarize(sampleReport(false))
	if len(e.Addons) != 1 {
		t.Fatalf("got %d addons", len(e.Addons))
	}
	a := e.Addons[0]
	if a.Key != "com.stremio.torrentio@host.example" {
		t.Errorf("key = %q", a.Key)
	}
	stream := a.Checks[1]
	if stream.Kind != scan.KindStream || stream.Runs != 2 || stream.Failures != 1 ||
		stream.ErrKind != probe.ErrTimeout || stream.MedianMs != 4000 {
		t.Errorf("stream summary = %+v", stream)
	}
}

func entry(at time.Time, statuses map[string]diagnose.Severity) Entry {
	e := Entry{Time: at}
	for name, s := range statuses {
		e.Addons = append(e.Addons, AddonSummary{Key: name + "@h", Name: name, Status: s,
			Checks: []CheckSummary{{Kind: scan.KindStream, MedianMs: 1000, Runs: 1}}})
	}
	return e
}

func TestStoreRoundTripSkipsBadLinesAndPrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history.jsonl")
	now := time.Now().UTC().Truncate(time.Second)
	old := entry(now.Add(-48*time.Hour), map[string]diagnose.Severity{"A": diagnose.OK})
	recent := entry(now, map[string]diagnose.Severity{"A": diagnose.Fail})

	if got, err := Load(path, time.Time{}); err != nil || got != nil {
		t.Fatalf("missing file: got %v, %v", got, err)
	}
	if err := Append(path, old); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("{not json\n")
	f.Close()
	if err := Append(path, recent); err != nil {
		t.Fatal(err)
	}

	all, err := Load(path, time.Time{})
	if err != nil || len(all) != 2 {
		t.Fatalf("Load: %d entries, %v", len(all), err)
	}
	if all[1].Addons[0].Status != diagnose.Fail {
		t.Errorf("severity did not round-trip: %v", all[1].Addons[0].Status)
	}

	kept, err := Prune(path, now.Add(-24*time.Hour))
	if err != nil || len(kept) != 1 {
		t.Fatalf("Prune: %d kept, %v", len(kept), err)
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != 1 {
		t.Errorf("file has %d lines after prune, want 1:\n%s", n, data)
	}
}

func TestTransitions(t *testing.T) {
	prev := entry(time.Now(), map[string]diagnose.Severity{"Same": diagnose.OK, "Broke": diagnose.OK, "Gone": diagnose.Warn})
	cur := entry(time.Now(), map[string]diagnose.Severity{"Same": diagnose.OK, "Broke": diagnose.Fail, "New": diagnose.OK})
	got := map[string]Change{}
	for _, c := range Transitions(prev, cur) {
		got[c.Name] = c
	}
	if len(got) != 3 {
		t.Fatalf("got %d changes: %+v", len(got), got)
	}
	if c := got["Broke"]; c.From != diagnose.OK || c.To != diagnose.Fail {
		t.Errorf("Broke: %+v", c)
	}
	if !got["New"].Added || !got["Gone"].Removed {
		t.Errorf("added/removed wrong: %+v", got)
	}
}

func TestView(t *testing.T) {
	t0 := time.Now()
	entries := []Entry{
		entry(t0, map[string]diagnose.Severity{"A": diagnose.Fail}),
		entry(t0.Add(time.Minute), map[string]diagnose.Severity{"A": diagnose.OK, "B": diagnose.OK}),
		entry(t0.Add(2*time.Minute), map[string]diagnose.Severity{"A": diagnose.OK, "B": diagnose.Warn}),
	}
	if View(entries[:1], 10) != nil {
		t.Error("one entry should give no view")
	}
	v := View(entries, 2)
	if v.Scans != 3 || len(v.Rows) != 2 {
		t.Fatalf("view = %+v", v)
	}
	rows := map[string]report.HistoryRow{}
	for _, r := range v.Rows {
		rows[r.Name] = r
	}
	if a := rows["A"]; int(a.Uptime) != 66 || len(a.Cells) != 2 || a.StreamMs != 1000 {
		t.Errorf("A = %+v", a)
	}
	if b := rows["B"]; b.Uptime != 100 || !b.Cells[0].Present || b.Cells[1].Status != diagnose.Warn {
		t.Errorf("B = %+v", b)
	}
}
