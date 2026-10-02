package probe

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetRecordsTimingPhases(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != UserAgent {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport.(*http.Transport).DisableKeepAlives = true

	res, body := Get(context.Background(), client, srv.URL)
	if !res.OK() || string(body) != `{"ok":true}` {
		t.Fatalf("res=%+v body=%s", res, body)
	}
	tm := res.Timing
	// Loopback connects can be below the clock resolution on Windows, so only
	// the TLS handshake and the server delay are asserted.
	if tm.TLS <= 0 || tm.Server() < 40*time.Millisecond || tm.Total < tm.TTFB {
		t.Fatalf("timing = %+v", tm)
	}
	if res.RemoteIP != "127.0.0.1" {
		t.Errorf("remote ip = %q", res.RemoteIP)
	}
}

func TestGetClassifiesUntrustedCertAsTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()

	res, _ := Get(context.Background(), NewClient(5*time.Second), srv.URL+"/secret-config/manifest.json")
	if res.ErrKind != ErrTLS {
		t.Fatalf("kind = %q (%s)", res.ErrKind, res.Err)
	}
	if strings.Contains(res.Err, "secret-config") {
		t.Fatalf("error message leaks the URL: %s", res.Err)
	}
}

func TestGetMarksNon2xxAsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Ray", "abc")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	res, _ := Get(context.Background(), NewClient(5*time.Second), srv.URL)
	if res.OK() || res.ErrKind != ErrHTTP || res.Status != 403 || !res.Cloudflare {
		t.Fatalf("res = %+v", res)
	}
}
