package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/diagnose"
	"github.com/dan-yates1/stremio-doctor/internal/probe"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

const secret = "realdebrid=SECRETKEY123"

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"https://v3-cinemeta.strem.io/manifest.json":                           "https://v3-cinemeta.strem.io/manifest.json",
		"https://torrentio.strem.fun/" + secret + "/manifest.json":             "https://torrentio.strem.fun/…/manifest.json",
		"https://x.io/" + secret + "/stream/movie/tt0111161.json":              "https://x.io/…/stream/movie/tt0111161.json",
		"https://x.io/a/b/catalog/movie/top/skip=0.json":                       "https://x.io/…/catalog/movie/top/skip=0.json",
		"https://x.io/manifest.json?key=" + secret:                             "https://x.io/manifest.json?…",
		"https://user:pass@x.io/" + secret + "/stream/series/tt1%3A1%3A1.json": "https://x.io/…/stream/series/tt1%3A1%3A1.json",
		"https://x.io/" + secret:                                               "https://x.io/…/",
		"garbage":                                                              "[redacted]",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func sampleInput(show bool) Input {
	base := "https://host.example/" + secret
	res := scan.AddonResult{
		Name: "Torrent<script>", Host: "host.example", TransportURL: base + "/manifest.json",
		Checks: []scan.Check{
			{Kind: scan.KindManifest, Label: "manifest", URL: base + "/manifest.json",
				Results: []probe.Result{{Status: 200, Timing: probe.Timing{Total: 80 * time.Millisecond, TTFB: 70 * time.Millisecond}}}, Items: []int{-1}},
			{Kind: scan.KindStream, Label: "stream movie tt0111161", URL: base + "/stream/movie/tt0111161.json",
				Results: []probe.Result{{Status: 200, Timing: probe.Timing{Total: 4 * time.Second, TTFB: 3900 * time.Millisecond}}}, Items: []int{12}},
		},
	}
	return Input{Version: "test", Source: "test", Rounds: 1, Results: []scan.AddonResult{res}, ShowURLs: show,
		Baseline: []scan.Baseline{{Name: "Local streaming server", Note: "not running"}}}
}

func TestReportsNeverLeakSecretsByDefault(t *testing.T) {
	r := Build(sampleInput(false))
	var term, html, js bytes.Buffer
	WriteTerminal(&term, r, ANSI)
	if err := WriteHTML(&html, r); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(&js, r); err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"terminal": term.String(), "html": html.String(), "json": js.String()} {
		if strings.Contains(out, "SECRETKEY") {
			t.Errorf("%s output leaks the secret", name)
		}
	}
	if strings.Contains(html.String(), "<script>") {
		t.Error("HTML does not escape addon names")
	}
	if !strings.Contains(term.String(), "Slow stream") {
		t.Errorf("terminal output missing finding:\n%s", term.String())
	}
}

func TestShowURLsKeepsFullURLs(t *testing.T) {
	r := Build(sampleInput(true))
	var html bytes.Buffer
	if err := WriteHTML(&html, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "SECRETKEY") || !strings.Contains(html.String(), "Do not share") {
		t.Error("expected full URLs and a warning with --show-urls")
	}
}

func TestHTMLRendersHistory(t *testing.T) {
	r := Build(sampleInput(false))
	var without bytes.Buffer
	if err := WriteHTML(&without, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without.String(), "<h2>History</h2>") {
		t.Error("history section shown without history")
	}

	at := time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC)
	r.History = &HistoryView{From: at, Scans: 2, Rows: []HistoryRow{{Name: "A<b>", Host: "h", Uptime: 50, StreamMs: 1500,
		Cells: []HistoryCell{{Time: at, Present: true, Status: diagnose.Fail}, {Time: at, Present: false}}}}}
	var html bytes.Buffer
	if err := WriteHTML(&html, r); err != nil {
		t.Fatal(err)
	}
	out := html.String()
	for _, want := range []string{"<h2>History</h2>", `class="s-fail"`, `class="s-none"`, "50%", "1.5s", "A&lt;b&gt;"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}
