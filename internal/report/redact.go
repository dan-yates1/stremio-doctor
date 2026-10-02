package report

import (
	"net/url"
	"strings"
)

// resourceSegments mark where the addon's own path ends and the Stremio
// protocol part begins. Everything before them is addon configuration, which
// commonly holds debrid API keys, so it is hidden.
var resourceSegments = map[string]bool{
	"manifest.json": true, "catalog": true, "meta": true, "stream": true,
	"subtitles": true, "addon_catalog": true,
}

// Redact hides configuration path segments, query strings and credentials:
// https://host/realdebrid=KEY/manifest.json → https://host/…/manifest.json
func Redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[redacted]"
	}
	segs := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	cut := len(segs)
	for i := len(segs) - 1; i >= 0; i-- {
		if resourceSegments[segs[i]] {
			cut = i
			break
		}
	}
	// Only "stream/movie/tt..json"-style tails are kept; manifest.json is its own tail.
	if cut < len(segs) && segs[cut] != "manifest.json" && cut+3 != len(segs) && cut+4 != len(segs) {
		cut = len(segs)
	}
	var b strings.Builder
	b.WriteString(u.Scheme + "://" + u.Host + "/")
	if cut > 0 && !(cut == 1 && segs[0] == "") {
		b.WriteString("…/")
	}
	b.WriteString(strings.Join(segs[cut:], "/"))
	if u.RawQuery != "" {
		b.WriteString("?…")
	}
	return b.String()
}
