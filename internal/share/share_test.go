package share

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/probe"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

const secret = "SECRETKEY123"

func result(name, id, transport, host string) scan.AddonResult {
	return scan.AddonResult{Name: name, ID: id, Version: "1.0.0", Host: host, TransportURL: transport,
		IPs: []string{"203.0.113.7"},
		Checks: []scan.Check{{Kind: scan.KindStream, URL: transport,
			Results: []probe.Result{{Status: 200, RemoteIP: "203.0.113.7", Timing: probe.Timing{Total: 2 * time.Second}}}, Items: []int{5}}}}
}

func sampleReport(showURLs bool) report.Report {
	results := []scan.AddonResult{
		result("Torrentio", "com.stremio.torrentio.addon", "https://torrentio.strem.fun/realdebrid="+secret+"/manifest.json", "torrentio.strem.fun"),
		result("My Comet", "comet", "https://dan-comet.elfhosted.com/"+secret+"/manifest.json", "dan-comet.elfhosted.com"),
		result("Home", "home.addon", "http://192.168.1.20:7000/manifest.json?key="+secret, "192.168.1.20"),
		result("Local", "local.addon", "http://localhost:7000/manifest.json", "localhost"),
		result("Broken", "", "https://v3-cinemeta.strem.io/manifest.json", "v3-cinemeta.strem.io"), // no manifest id
	}
	return report.Build(report.Input{Version: "test", Rounds: 1, Results: results, ShowURLs: showURLs,
		Baseline: []scan.Baseline{{Name: scan.BaselineTargets[0].Name, Result: probe.Result{Status: 200, Timing: probe.Timing{Total: 30 * time.Millisecond}}}}})
}

func TestSharePayloadHasNoSecrets(t *testing.T) {
	for _, show := range []bool{false, true} {
		p, ok := Build(sampleReport(show), "v1.2.3")
		if !ok {
			t.Fatal("expected something to share")
		}
		raw, _ := json.Marshal(p)
		s := string(raw)
		for _, leak := range []string{secret, "dan-comet", "192.168.1.20", "localhost", "203.0.113.7", "Torrentio", "manifest.json", "My Comet"} {
			if strings.Contains(s, leak) {
				t.Errorf("showURLs=%v: payload contains %q: %s", show, leak, s)
			}
		}
		if len(p.Addons) != 1 || p.Addons[0].Host != "torrentio.strem.fun" || p.Addons[0].ID != "com.stremio.torrentio.addon" {
			t.Errorf("addons = %+v", p.Addons)
		}
		if p.Baseline.InternetMs != 30 || p.Baseline.StremioAPIMs != -1 {
			t.Errorf("baseline = %+v", p.Baseline)
		}
		if p.Hour.Minute() != 0 || p.Hour.Second() != 0 || p.Hour.Location() != time.UTC {
			t.Errorf("hour not truncated to UTC hour: %v", p.Hour)
		}
	}
}

func TestBuildNothingToShare(t *testing.T) {
	rep := report.Build(report.Input{Results: []scan.AddonResult{result("Home", "x", "http://10.0.0.1/manifest.json", "10.0.0.1")}})
	if _, ok := Build(rep, "v"); ok {
		t.Error("private-only report should have nothing to share")
	}
}

func TestSend(t *testing.T) {
	var got Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p, _ := Build(sampleReport(false), "v1")
	if err := Send(context.Background(), srv.URL, p); err != nil {
		t.Fatal(err)
	}
	if got.Schema != SchemaVersion || len(got.Addons) != 1 {
		t.Errorf("server got %+v", got)
	}
}

func TestSendReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if err := Send(context.Background(), srv.URL, Payload{}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("err = %v", err)
	}
}
