// Package diagnose turns raw scan measurements into plain-English findings.
package diagnose

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/probe"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

// Severity of a finding. Higher is worse.
type Severity int

const (
	OK Severity = iota
	Info
	Warn
	Fail
)

var severityNames = [...]string{"ok", "info", "warn", "fail"}

func (s Severity) String() string { return severityNames[s] }

// MarshalJSON encodes the severity as its name.
func (s Severity) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// UnmarshalJSON decodes a severity name written by MarshalJSON.
func (s *Severity) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	for i, n := range severityNames {
		if n == name {
			*s = Severity(i)
			return nil
		}
	}
	return fmt.Errorf("unknown severity %q", name)
}

// Finding is one diagnosis.
type Finding struct {
	Severity Severity `json:"severity"`
	Addon    string   `json:"addon,omitempty"` // empty for findings about several addons
	Check    string   `json:"check,omitempty"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
}

// Limits are the slow / very-slow thresholds for one check kind.
type Limits struct{ Slow, VerySlow time.Duration }

// Thresholds per check kind. Stream requests block the stream list, so they
// get a little more slack than the lightweight resources.
var Thresholds = map[string]Limits{
	scan.KindManifest:  {Slow: 1500 * time.Millisecond, VerySlow: 5 * time.Second},
	scan.KindCatalog:   {Slow: 2 * time.Second, VerySlow: 6 * time.Second},
	scan.KindMeta:      {Slow: 2 * time.Second, VerySlow: 6 * time.Second},
	scan.KindStream:    {Slow: 3 * time.Second, VerySlow: 8 * time.Second},
	scan.KindSubtitles: {Slow: 2 * time.Second, VerySlow: 6 * time.Second},
}

// Addon diagnoses a single addon.
func Addon(a scan.AddonResult) []Finding {
	var fs []Finding
	add := func(f Finding) { f.Addon = a.Name; fs = append(fs, f) }

	if a.ManifestErr != "" {
		add(Finding{Severity: Fail, Check: scan.KindManifest, Title: "Invalid manifest",
			Detail: "The addon's manifest.json isn't a valid addon manifest. The addon is probably broken or the URL now points somewhere else."})
	}
	if f, ok := totalOutage(a); ok {
		add(f)
		return fs
	}
	var unsteady []string
	for _, c := range a.Checks {
		if f, ok := failure(c); ok {
			add(f)
			continue
		}
		if f, ok := slowness(c); ok {
			add(f)
		}
		if s, ok := variance(c); ok {
			unsteady = append(unsteady, s)
		}
	}
	if len(unsteady) > 0 {
		add(Finding{Severity: Info, Title: "Inconsistent response times",
			Detail: "Speed varied a lot between attempts (" + strings.Join(unsteady, "; ") + "), a sign of an overloaded or cold-starting server."})
	}
	if f, ok := noStreams(a); ok && !isLocal(a.Host) {
		add(f)
	}
	if insecure(a.TransportURL) {
		add(Finding{Severity: Info, Title: "Unencrypted HTTP",
			Detail: "This addon is installed over plain http://, so its traffic (including any keys in its URL) isn't encrypted."})
	}
	return fs
}

// Status is the worst severity among findings.
func Status(fs []Finding) Severity {
	worst := OK
	for _, f := range fs {
		if f.Severity > worst {
			worst = f.Severity
		}
	}
	return worst
}

// Sort orders findings worst first, keeping the original order otherwise.
func Sort(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Severity > fs[j].Severity })
}

// totalOutage collapses an addon where every request failed into a single
// finding based on the manifest error, instead of one finding per request.
func totalOutage(a scan.AddonResult) (Finding, bool) {
	m, ok := a.Find(scan.KindManifest)
	if !ok || !m.AllFailed() {
		return Finding{}, false
	}
	for _, c := range a.Checks {
		if !c.AllFailed() {
			return Finding{}, false
		}
	}
	last, _ := m.LastError()
	f := allFailed(m, last)
	f.Severity, f.Check = Fail, scan.KindManifest
	if len(a.Checks) > 1 {
		f.Detail += " Every other request to it fails too, so Stremio gets nothing from this addon right now."
	}
	return f, true
}

func failure(c scan.Check) (Finding, bool) {
	last, failed := c.LastError()
	if !failed {
		return Finding{}, false
	}
	n, total := c.Failures(), len(c.Results)
	if n < total {
		return Finding{Severity: Warn, Check: c.Kind, Title: "Flaky " + c.Kind + " requests",
			Detail: fmt.Sprintf("%d of %d attempts for %s failed (last: %s). Stremio will sometimes show nothing from this addon.", n, total, c.Label, describe(last))}, true
	}
	f := allFailed(c, last)
	f.Check = c.Kind
	return f, true
}

func allFailed(c scan.Check, r probe.Result) Finding {
	what := c.Kind + " requests"
	switch r.ErrKind {
	case probe.ErrTimeout:
		return Finding{Severity: Fail, Title: what + " time out",
			Detail: fmt.Sprintf("No response for %s on any attempt. Stremio will keep spinning, then show nothing from this addon.", c.Label)}
	case probe.ErrDNS:
		return Finding{Severity: Fail, Title: "Domain doesn't resolve",
			Detail: "The addon's domain name doesn't exist anymore (or your DNS can't find it). The addon has likely shut down or moved; reinstall it from its current site."}
	case probe.ErrConnect:
		return Finding{Severity: Fail, Title: "Host refuses connections",
			Detail: "The server is offline or blocking you (" + r.Err + ")."}
	case probe.ErrTLS:
		return Finding{Severity: Fail, Title: "TLS / certificate error",
			Detail: "Secure connection failed: " + r.Err + ". The addon's certificate may have expired."}
	}
	switch s := r.Status; {
	case s == 429:
		return Finding{Severity: Fail, Title: "Rate limited (HTTP 429)",
			Detail: "The addon is rejecting requests because too many are coming in, typical for busy public instances. A private or self-hosted instance avoids this."}
	case (s == 403 || s == 503) && r.Cloudflare:
		return Finding{Severity: Fail, Title: "Blocked by Cloudflare",
			Detail: fmt.Sprintf("Cloudflare answered HTTP %d instead of the addon, usually a bot challenge or your IP/VPN being blocked.", s)}
	case s == 401 || s == 403:
		return Finding{Severity: Fail, Title: fmt.Sprintf("Access denied (HTTP %d)", s),
			Detail: "Usually an expired or wrong API / debrid key in the addon's configuration. Reconfigure and reinstall it."}
	case s == 404 && c.Kind == scan.KindManifest:
		return Finding{Severity: Fail, Title: "Manifest not found (HTTP 404)",
			Detail: "The installed addon URL returns 404 Not Found. If this persists, the instance was removed or moved: reinstall it from its current configure page."}
	case s == 404:
		return Finding{Severity: Info, Title: fmt.Sprintf("No %s for sample title (HTTP 404)", c.Kind),
			Detail: fmt.Sprintf("%s returned 404. Some addons do this when they have nothing for a title; it's only a problem if it happens for everything.", c.Label)}
	case s >= 500:
		return Finding{Severity: Fail, Title: fmt.Sprintf("Server error (HTTP %d)", s),
			Detail: "The addon itself is crashing or overloaded. Nothing to fix on your side; try again later or use an alternative instance."}
	}
	return Finding{Severity: Fail, Title: what + " fail", Detail: describe(r)}
}

func slowness(c scan.Check) (Finding, bool) {
	lim, ok := Thresholds[c.Kind]
	if !ok {
		return Finding{}, false
	}
	m := c.Median()
	sev := OK
	switch {
	case m.Total >= lim.VerySlow:
		sev = Fail
	case m.Total >= lim.Slow:
		sev = Warn
	default:
		return Finding{}, false
	}
	title := "Slow " + c.Kind
	if sev == Fail {
		title = "Very slow " + c.Kind
	}
	return Finding{Severity: sev, Check: c.Kind, Title: title,
		Detail: fmt.Sprintf("%s takes %s (median; %s). %s", c.Label, Dur(m.Total), phases(m), blame(c.Kind, m))}, true
}

// Breakdown formats a timing as "6.8s: network 0.3s, server 6.1s, download 0.4s".
func Breakdown(t probe.Timing) string {
	return Dur(t.Total) + ": " + phases(t)
}

func phases(t probe.Timing) string {
	return fmt.Sprintf("network %s, server %s, download %s", Dur(t.Network()), Dur(t.Server()), Dur(t.Download()))
}

func blame(kind string, t probe.Timing) string {
	if t.Total <= 0 {
		return ""
	}
	share := func(d time.Duration) float64 { return float64(d) / float64(t.Total) }
	switch {
	case share(t.Server()) >= 0.6 && kind == scan.KindStream:
		return "Most of that is the addon's server working, so the addon (or the debrid service / indexers it queries) is the bottleneck, not your connection."
	case share(t.Server()) >= 0.6:
		return "Most of that is the addon's server working, so the addon is the bottleneck, not your connection."
	case share(t.Network()) >= 0.5:
		return "Most of that is just connecting (DNS/TCP/TLS), which points at your network, DNS, VPN, or a far-away host."
	case share(t.Download()) >= 0.5:
		return "Most of that is downloading a large response; the addon returns a lot of results."
	}
	return "Time is spread across connecting and server processing."
}

// variance describes a check whose slowest attempt took over 2s and at
// least 3x its fastest, e.g. "meta 859ms–3.8s".
func variance(c scan.Check) (string, bool) {
	lo, hi := c.MinMaxTotal()
	if lo <= 0 || hi < 2*time.Second || hi < 3*lo {
		return "", false
	}
	return fmt.Sprintf("%s %s–%s", c.Label, Dur(lo), Dur(hi)), true
}

func noStreams(a scan.AddonResult) (Finding, bool) {
	seen := false
	for _, c := range a.Checks {
		if c.Kind != scan.KindStream {
			continue
		}
		if c.Failures() > 0 || c.MaxItems() != 0 {
			return Finding{}, false
		}
		seen = true
	}
	if !seen {
		return Finding{}, false
	}
	return Finding{Severity: Info, Check: scan.KindStream, Title: "No streams for well-known titles",
		Detail: "Returned 0 streams for The Shawshank Redemption and/or Game of Thrones S01E01. That's normal for niche addons (anime, regional), otherwise check its configuration (debrid key, providers, filters)."}, true
}

func insecure(transport string) bool {
	u, err := url.Parse(transport)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if isLocal(host) {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsPrivate()
}

// isLocal reports whether host is this computer (e.g. Stremio's Local Files addon).
func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func describe(r probe.Result) string {
	if r.Err != "" {
		return r.Err
	}
	return fmt.Sprintf("HTTP %d", r.Status)
}

// Dur formats a duration compactly: 850ms, 1.2s.
func Dur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
