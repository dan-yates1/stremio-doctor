package scan

import (
	"context"
	"sync"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/probe"
)

// BaselineTarget is a reference service checked alongside the addons so a
// slow result can be blamed on the addon or on the local network.
type BaselineTarget struct {
	Name string
	URL  string
	Note string // shown when unreachable
}

// BaselineTargets are checked on every run.
var BaselineTargets = []BaselineTarget{
	{Name: "Internet (Cloudflare)", URL: "https://1.1.1.1/cdn-cgi/trace",
		Note: "General internet connectivity looks broken; fix that first."},
	{Name: "Stremio API", URL: "https://api.strem.io/api/",
		Note: "Stremio's own API is unreachable: login, sync and the addon list may fail."},
	{Name: "Local streaming server", URL: "http://127.0.0.1:11470/settings",
		Note: "Stremio's streaming server isn't running (start Stremio). Torrent streams need it."},
}

// Baseline is the result for one reference service.
type Baseline struct {
	Name   string       `json:"name"`
	URL    string       `json:"url"`
	Note   string       `json:"note"`
	Result probe.Result `json:"result"`
}

// Reachable reports whether the service answered at all. Any non-5xx HTTP
// response counts: the API root returns 404 but proves the host is fine.
func (b Baseline) Reachable() bool { return b.Result.Status > 0 && b.Result.Status < 500 }

// RunBaseline checks targets concurrently.
func RunBaseline(ctx context.Context, targets []BaselineTarget, timeout time.Duration) []Baseline {
	client := probe.NewClient(timeout)
	out := make([]Baseline, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t BaselineTarget) {
			defer wg.Done()
			res, _ := probe.Get(ctx, client, t.URL)
			out[i] = Baseline{Name: t.Name, URL: t.URL, Note: t.Note, Result: res}
		}(i, t)
	}
	wg.Wait()
	return out
}
