// Package scan runs the probe requests Stremio would make against each
// installed addon, repeated over several rounds.
package scan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/addon"
	"github.com/dan-yates1/stremio-doctor/internal/probe"
)

// Check kinds, matching Stremio addon resources.
const (
	KindManifest  = "manifest"
	KindCatalog   = "catalog"
	KindMeta      = "meta"
	KindStream    = "stream"
	KindSubtitles = "subtitles"
)

// Well-known sample titles used for stream/subtitle requests.
const (
	SampleMovie  = "tt0111161"     // The Shawshank Redemption
	SampleSeries = "tt0944947:1:1" // Game of Thrones S01E01
)

// Options controls a scan.
type Options struct {
	Rounds      int
	Timeout     time.Duration
	Concurrency int
}

// DefaultOptions are the CLI defaults.
var DefaultOptions = Options{Rounds: 3, Timeout: 15 * time.Second, Concurrency: 8}

// Check is one request type repeated across rounds.
type Check struct {
	Kind    string         `json:"kind"`
	Label   string         `json:"label"`
	URL     string         `json:"url"`
	Results []probe.Result `json:"results"`
	Items   []int          `json:"items"` // item count per result, -1 when not applicable

	firstItem item // first catalog item, used to pick the meta request
}

// AddonResult is everything measured for one addon.
type AddonResult struct {
	Index        int      `json:"index"`
	Name         string   `json:"name"`
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	TransportURL string   `json:"transportUrl"`
	Host         string   `json:"host"`
	IPs          []string `json:"ips,omitempty"`
	Official     bool     `json:"official"`
	ManifestErr  string   `json:"manifestError,omitempty"`
	Checks       []Check  `json:"checks"`
}

// Run scans every addon concurrently. progress, if non-nil, is called after
// each addon completes.
func Run(ctx context.Context, addons []addon.Installed, opts Options, progress func(done, total int)) []AddonResult {
	opts = withDefaults(opts)
	client := probe.NewClient(opts.Timeout)
	out := make([]AddonResult, len(addons))
	sem := make(chan struct{}, opts.Concurrency)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
	)
	for i, a := range addons {
		wg.Add(1)
		go func(i int, a addon.Installed) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = scanAddon(ctx, client, i, a, opts.Rounds)
			if progress != nil {
				mu.Lock()
				done++
				progress(done, len(addons))
				mu.Unlock()
			}
		}(i, a)
	}
	wg.Wait()
	return out
}

func withDefaults(o Options) Options {
	if o.Rounds < 1 {
		o.Rounds = DefaultOptions.Rounds
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultOptions.Timeout
	}
	if o.Concurrency < 1 {
		o.Concurrency = DefaultOptions.Concurrency
	}
	return o
}

func scanAddon(ctx context.Context, client *http.Client, idx int, a addon.Installed, rounds int) AddonResult {
	r := AddonResult{Index: idx, TransportURL: a.TransportURL, Official: a.Flags.Official}
	if u, err := url.Parse(a.TransportURL); err == nil {
		r.Host = u.Hostname()
	}

	manifest := &Check{Kind: KindManifest, Label: "manifest", URL: a.TransportURL}
	res, body := probe.Get(ctx, client, a.TransportURL)
	manifest.add(res, -1)

	m := a.Manifest
	if res.OK() {
		var fetched addon.Manifest
		if err := json.Unmarshal(body, &fetched); err != nil || fetched.ID == "" {
			r.ManifestErr = "manifest.json is not a valid addon manifest"
		} else {
			m = &fetched
		}
	}
	setIdentity(&r, a, m)

	var checks []*Check
	// Skip resource checks when the host can't be reached at all: every one
	// would just wait for the same failure.
	if m != nil && !unreachable(res) {
		checks = firstRound(ctx, client, a.TransportURL, m)
	}

	for round := 1; round < rounds; round++ {
		if ctx.Err() != nil {
			break
		}
		all := append([]*Check{manifest}, checks...)
		runParallel(ctx, client, all)
	}

	r.Checks = append(r.Checks, *manifest)
	for _, c := range checks {
		r.Checks = append(r.Checks, *c)
	}
	r.IPs = collectIPs(r.Checks)
	return r
}

func setIdentity(r *AddonResult, a addon.Installed, m *addon.Manifest) {
	r.Name = a.DisplayName()
	if m != nil {
		r.ID, r.Version = m.ID, m.Version
		if m.Name != "" {
			r.Name = m.Name
		}
	}
}

// firstRound plans and runs the resource checks once. The meta check depends
// on the catalog response (it fetches the first item the catalog returned).
func firstRound(ctx context.Context, client *http.Client, transport string, m *addon.Manifest) []*Check {
	var catalog, meta *Check
	var independent []*Check

	if c, ok := m.BrowsableCatalog(); ok && m.HasResource(KindCatalog) {
		catalog = &Check{Kind: KindCatalog, Label: "catalog " + c.Type + "/" + c.ID,
			URL: addon.ResourceURL(transport, KindCatalog, c.Type, c.ID)}
		independent = append(independent, catalog)
	}
	for _, s := range []struct{ typ, id string }{{"movie", SampleMovie}, {"series", SampleSeries}} {
		if m.Supports(KindStream, s.typ, s.id) {
			independent = append(independent, &Check{Kind: KindStream, Label: "stream " + s.typ + " " + s.id,
				URL: addon.ResourceURL(transport, KindStream, s.typ, s.id)})
		}
	}
	if m.Supports(KindSubtitles, "movie", SampleMovie) {
		independent = append(independent, &Check{Kind: KindSubtitles, Label: "subtitles movie " + SampleMovie,
			URL: addon.ResourceURL(transport, KindSubtitles, "movie", SampleMovie)})
	}

	runParallel(ctx, client, independent)

	if typ, id, ok := metaTarget(m, catalog); ok {
		meta = &Check{Kind: KindMeta, Label: "meta " + typ + " " + id, URL: addon.ResourceURL(transport, KindMeta, typ, id)}
		runParallel(ctx, client, []*Check{meta})
	}

	var checks []*Check
	if catalog != nil {
		checks = append(checks, catalog)
	}
	if meta != nil {
		checks = append(checks, meta)
	}
	for _, c := range independent {
		if c != catalog {
			checks = append(checks, c)
		}
	}
	return checks
}

// metaTarget picks an id to request meta for: the first catalog item if the
// addon serves meta for it, otherwise the sample movie.
func metaTarget(m *addon.Manifest, catalog *Check) (typ, id string, ok bool) {
	if catalog != nil && catalog.firstItem.id != "" && m.Supports(KindMeta, catalog.firstItem.typ, catalog.firstItem.id) {
		return catalog.firstItem.typ, catalog.firstItem.id, true
	}
	if m.Supports(KindMeta, "movie", SampleMovie) {
		return "movie", SampleMovie, true
	}
	return "", "", false
}

func runParallel(ctx context.Context, client *http.Client, checks []*Check) {
	var wg sync.WaitGroup
	for _, c := range checks {
		wg.Add(1)
		go func(c *Check) {
			defer wg.Done()
			res, body := probe.Get(ctx, client, c.URL)
			c.add(res, countItems(c, body, res.OK()))
		}(c)
	}
	wg.Wait()
}

func (c *Check) add(res probe.Result, items int) {
	c.Results = append(c.Results, res)
	c.Items = append(c.Items, items)
}

// countItems parses a response body and returns how many items it holds.
// It also records the first catalog item for the meta check.
func countItems(c *Check, body []byte, ok bool) int {
	if !ok || c.Kind == KindManifest {
		return -1
	}
	var payload struct {
		Metas []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"metas"`
		Meta      json.RawMessage   `json:"meta"`
		Streams   []json.RawMessage `json:"streams"`
		Subtitles []json.RawMessage `json:"subtitles"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return -1
	}
	switch c.Kind {
	case KindCatalog:
		if len(payload.Metas) > 0 && c.firstItem.id == "" {
			c.firstItem = item{typ: payload.Metas[0].Type, id: payload.Metas[0].ID}
		}
		return len(payload.Metas)
	case KindStream:
		return len(payload.Streams)
	case KindSubtitles:
		return len(payload.Subtitles)
	case KindMeta:
		if len(payload.Meta) == 0 || string(payload.Meta) == "null" {
			return 0
		}
		return 1
	}
	return -1
}

type item struct{ typ, id string }

// unreachable reports whether a failure means no HTTP conversation happened.
func unreachable(r probe.Result) bool {
	switch r.ErrKind {
	case probe.ErrDNS, probe.ErrConnect, probe.ErrTLS:
		return true
	}
	return false
}

func collectIPs(checks []Check) []string {
	seen := map[string]bool{}
	var ips []string
	for _, c := range checks {
		for _, r := range c.Results {
			if r.RemoteIP != "" && !seen[r.RemoteIP] {
				seen[r.RemoteIP] = true
				ips = append(ips, r.RemoteIP)
			}
		}
	}
	sort.Strings(ips)
	return ips
}
