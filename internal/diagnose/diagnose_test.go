package diagnose

import (
	"strings"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/probe"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

func ok(total, network time.Duration) probe.Result {
	return probe.Result{Status: 200, Timing: probe.Timing{
		DNS: network / 3, Connect: network / 3, TLS: network / 3, TTFB: total, Total: total}}
}

func check(kind string, rs ...probe.Result) scan.Check {
	items := make([]int, len(rs))
	for i := range items {
		items[i] = 1
	}
	return scan.Check{Kind: kind, Label: kind, Results: rs, Items: items}
}

func addonWith(name string, checks ...scan.Check) scan.AddonResult {
	return scan.AddonResult{Name: name, Host: "a.example.com", TransportURL: "https://a.example.com/manifest.json", Checks: checks}
}

func only(t *testing.T, fs []Finding) Finding {
	t.Helper()
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(fs), fs)
	}
	return fs[0]
}

func TestHealthyAddonHasNoFindings(t *testing.T) {
	a := addonWith("A", check(scan.KindManifest, ok(100*time.Millisecond, 50*time.Millisecond)))
	if fs := Addon(a); len(fs) != 0 {
		t.Fatalf("got %+v", fs)
	}
}

func TestFailureKinds(t *testing.T) {
	cases := []struct {
		name  string
		kind  string
		res   probe.Result
		sev   Severity
		title string
	}{
		{"timeout", scan.KindStream, probe.Result{ErrKind: probe.ErrTimeout, Err: "timed out"}, Fail, "time out"},
		{"dns", scan.KindManifest, probe.Result{ErrKind: probe.ErrDNS, Err: "no such host"}, Fail, "doesn't resolve"},
		{"429", scan.KindStream, probe.Result{Status: 429, ErrKind: probe.ErrHTTP}, Fail, "Rate limited"},
		{"cloudflare", scan.KindStream, probe.Result{Status: 403, ErrKind: probe.ErrHTTP, Cloudflare: true}, Fail, "Cloudflare"},
		{"403", scan.KindStream, probe.Result{Status: 403, ErrKind: probe.ErrHTTP}, Fail, "Access denied"},
		{"404 manifest", scan.KindManifest, probe.Result{Status: 404, ErrKind: probe.ErrHTTP}, Fail, "Manifest not found"},
		{"404 stream", scan.KindStream, probe.Result{Status: 404, ErrKind: probe.ErrHTTP}, Info, "No stream"},
		{"500", scan.KindCatalog, probe.Result{Status: 502, ErrKind: probe.ErrHTTP}, Fail, "Server error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := only(t, Addon(addonWith("A", check(c.kind, c.res, c.res))))
			if f.Severity != c.sev || !strings.Contains(f.Title, c.title) {
				t.Fatalf("got %v %q, want %v containing %q", f.Severity, f.Title, c.sev, c.title)
			}
		})
	}
}

func TestFlakyIsWarning(t *testing.T) {
	bad := probe.Result{ErrKind: probe.ErrTimeout, Err: "timed out"}
	f := only(t, Addon(addonWith("A", check(scan.KindStream, ok(time.Second/2, 0), bad, ok(time.Second/2, 0)))))
	if f.Severity != Warn || !strings.Contains(f.Detail, "1 of 3") {
		t.Fatalf("got %+v", f)
	}
}

func TestSlowStreamBlamesServer(t *testing.T) {
	r := ok(5*time.Second, 300*time.Millisecond)
	f := only(t, Addon(addonWith("A", check(scan.KindStream, r, r, r))))
	if f.Severity != Warn || !strings.Contains(f.Detail, "server") || !strings.Contains(f.Detail, "not your connection") {
		t.Fatalf("got %+v", f)
	}
}

func TestVerySlowNetworkBlamesNetwork(t *testing.T) {
	r := ok(9*time.Second, 7*time.Second)
	f := only(t, Addon(addonWith("A", check(scan.KindStream, r, r, r))))
	if f.Severity != Fail || !strings.Contains(f.Detail, "your network") {
		t.Fatalf("got %+v", f)
	}
}

func TestZeroStreamsIsInfo(t *testing.T) {
	a := addonWith("A", check(scan.KindStream, ok(time.Second/2, 0)))
	a.Checks[0].Items = []int{0}
	f := only(t, Addon(a))
	if f.Severity != Info || !strings.Contains(f.Title, "No streams") {
		t.Fatalf("got %+v", f)
	}
}

func TestInsecureHTTPButNotLocal(t *testing.T) {
	a := addonWith("A")
	a.TransportURL = "http://public.example.com/manifest.json"
	if f := only(t, Addon(a)); !strings.Contains(f.Title, "Unencrypted") {
		t.Fatalf("got %+v", f)
	}
	a.TransportURL = "http://127.0.0.1:11470/local-addon/manifest.json"
	if fs := Addon(a); len(fs) != 0 {
		t.Fatalf("local addon flagged: %+v", fs)
	}
}

func TestGlobalDuplicatesAndSharedHostDown(t *testing.T) {
	down := check(scan.KindManifest, probe.Result{ErrKind: probe.ErrConnect, Err: "refused"})
	all := []scan.AddonResult{
		{Name: "X", ID: "x", Host: "one.elfhosted.com", Checks: []scan.Check{down}},
		{Name: "Y", ID: "y", Host: "two.elfhosted.com", Checks: []scan.Check{down}},
		{Name: "Z", ID: "x", Host: "other.net", Checks: []scan.Check{check(scan.KindManifest, ok(time.Second/10, 0))}},
	}
	fs := Global(all)
	var dup, host bool
	for _, f := range fs {
		dup = dup || strings.Contains(f.Title, "installed 2 times")
		host = host || (f.Title == "Host elfhosted.com is down" && f.Severity == Fail)
	}
	if !dup || !host {
		t.Fatalf("dup=%v host=%v findings=%+v", dup, host, fs)
	}
}

func TestSlowestStreamsSummary(t *testing.T) {
	slow, fast := ok(6*time.Second, 0), ok(time.Second, 0)
	all := []scan.AddonResult{
		addonWith("Fast", check(scan.KindStream, fast)),
		addonWith("Slow", check(scan.KindStream, slow)),
	}
	all[1].Host = "b.other.org"
	f := only(t, Global(all))
	if !strings.Contains(f.Detail, "Slow (6.0s), Fast (1.0s)") {
		t.Fatalf("got %q", f.Detail)
	}
}

func TestSite(t *testing.T) {
	for in, want := range map[string]string{
		"a.b.elfhosted.com":   "elfhosted.com",
		"torrentio.strem.fun": "strem.fun",
		"x.y.co.uk":           "y.co.uk",
		"127.0.0.1":           "127.0.0.1",
		"localhost":           "localhost",
		"example.com":         "example.com",
	} {
		if got := Site(in); got != want {
			t.Errorf("Site(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalAddonWithNoStreamsIsNotFlagged(t *testing.T) {
	a := addonWith("Local Files", check(scan.KindStream, ok(time.Millisecond, 0)))
	a.Host, a.TransportURL = "127.0.0.1", "http://127.0.0.1:11470/local-addon/manifest.json"
	a.Checks[0].Items = []int{0}
	if fs := Addon(a); len(fs) != 0 {
		t.Fatalf("got %+v", fs)
	}
}

func TestInconsistentTimesMergeIntoOneFinding(t *testing.T) {
	fast, slow := ok(500*time.Millisecond, 0), ok(3*time.Second, 0)
	a := addonWith("A", check(scan.KindManifest, fast, slow, fast), check(scan.KindMeta, fast, slow, fast))
	f := only(t, Addon(a))
	if f.Title != "Inconsistent response times" || !strings.Contains(f.Detail, "manifest 500ms–3.0s; meta 500ms–3.0s") {
		t.Fatalf("got %+v", f)
	}
}

func TestTotalOutageIsOneFinding(t *testing.T) {
	nf := probe.Result{Status: 404, ErrKind: probe.ErrHTTP}
	a := addonWith("A", check(scan.KindManifest, nf), check(scan.KindMeta, nf), check(scan.KindSubtitles, nf))
	f := only(t, Addon(a))
	if f.Severity != Fail || !strings.Contains(f.Title, "Manifest not found") || !strings.Contains(f.Detail, "Every other request") {
		t.Fatalf("got %+v", f)
	}
}
