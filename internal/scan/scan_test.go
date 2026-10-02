package scan

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/addon"
	"github.com/dan-yates1/stremio-doctor/internal/probe"
)

const fakeManifest = `{"id":"org.fake","name":"Fake","version":"2.0.0","types":["movie","series"],
"idPrefixes":["tt"],"resources":["catalog","meta","stream"],"catalogs":[{"type":"movie","id":"top"}]}`

// fakeAddon serves a working addon and records the paths it was asked for.
func fakeAddon(t *testing.T) (*httptest.Server, *[]string) {
	var (
		paths []string
		mu    sync.Mutex
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/manifest.json"):
			fmt.Fprint(w, fakeManifest)
		case strings.Contains(r.URL.Path, "/catalog/"):
			fmt.Fprint(w, `{"metas":[{"id":"tt0068646","type":"movie"},{"id":"tt1","type":"movie"}]}`)
		case strings.Contains(r.URL.Path, "/meta/"):
			fmt.Fprint(w, `{"meta":{"id":"tt0068646"}}`)
		case strings.Contains(r.URL.Path, "/stream/"):
			fmt.Fprint(w, `{"streams":[{"url":"a"},{"url":"b"},{"url":"c"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestRunProbesEveryDeclaredResource(t *testing.T) {
	srv, paths := fakeAddon(t)
	res := Run(context.Background(), []addon.Installed{{TransportURL: srv.URL + "/cfg/manifest.json"}},
		Options{Rounds: 2, Timeout: 5 * time.Second}, nil)[0]

	if res.Name != "Fake" || res.ID != "org.fake" || res.Version != "2.0.0" {
		t.Fatalf("identity = %q %q %q", res.Name, res.ID, res.Version)
	}
	kinds := map[string]Check{}
	for _, c := range res.Checks {
		kinds[c.Label] = c
		if len(c.Results) != 2 {
			t.Errorf("%s: %d results, want 2", c.Label, len(c.Results))
		}
		if c.Failures() != 0 {
			t.Errorf("%s: unexpected failures %+v", c.Label, c.Results)
		}
	}
	for _, label := range []string{"manifest", "catalog movie/top", "meta movie tt0068646",
		"stream movie " + SampleMovie, "stream series " + SampleSeries} {
		if _, ok := kinds[label]; !ok {
			t.Errorf("missing check %q (have %v)", label, *paths)
		}
	}
	if got := kinds["stream movie "+SampleMovie].MaxItems(); got != 3 {
		t.Errorf("stream items = %d, want 3", got)
	}
	if !strings.HasPrefix((*paths)[0], "/cfg/") {
		t.Errorf("config path not preserved: %v", *paths)
	}
}

func TestRunRecordsHTTPErrorsAndCloudflare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	res := Run(context.Background(), []addon.Installed{{TransportURL: srv.URL + "/manifest.json"}}, Options{Rounds: 1}, nil)[0]
	m, _ := res.Find(KindManifest)
	r := m.Results[0]
	if r.Status != 429 || r.ErrKind != probe.ErrHTTP || !r.Cloudflare {
		t.Fatalf("result = %+v", r)
	}
}

func TestRunSkipsResourceChecksWhenHostUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/manifest.json"
	srv.Close() // nothing listens any more

	var m addon.Manifest
	m.Resources = []addon.Resource{{Name: "stream"}}
	m.Types = []string{"movie"}
	res := Run(context.Background(), []addon.Installed{{TransportURL: url, Manifest: &m}}, Options{Rounds: 3, Timeout: 2 * time.Second}, nil)[0]

	if len(res.Checks) != 1 {
		t.Fatalf("checks = %d, want only manifest", len(res.Checks))
	}
	if got := res.Checks[0].Results[0].ErrKind; got != probe.ErrConnect {
		t.Fatalf("err kind = %q, want connect", got)
	}
}

func TestRunTimesOut(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)

	res := Run(context.Background(), []addon.Installed{{TransportURL: srv.URL + "/manifest.json"}},
		Options{Rounds: 1, Timeout: 200 * time.Millisecond}, nil)[0]
	if got := res.Checks[0].Results[0].ErrKind; got != probe.ErrTimeout {
		t.Fatalf("err kind = %q, want timeout", got)
	}
}

func TestMedianAndMinMax(t *testing.T) {
	c := Check{Results: []probe.Result{
		{Status: 200, Timing: probe.Timing{Total: 3 * time.Second}},
		{Status: 200, Timing: probe.Timing{Total: 1 * time.Second}},
		{Status: 500, Timing: probe.Timing{Total: 9 * time.Second}},
		{Status: 200, Timing: probe.Timing{Total: 2 * time.Second}},
	}}
	if got := c.Median().Total; got != 2*time.Second {
		t.Errorf("median = %v, want 2s (failures excluded)", got)
	}
	if lo, hi := c.MinMaxTotal(); lo != time.Second || hi != 3*time.Second {
		t.Errorf("minmax = %v %v", lo, hi)
	}
}
