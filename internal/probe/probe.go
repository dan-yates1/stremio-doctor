// Package probe performs single timed HTTP requests and classifies failures.
package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"time"
)

// MaxBody caps how much of a response is read (stream lists can be large).
const MaxBody = 20 << 20

// UserAgent is sent with every request so addon operators can identify us.
const UserAgent = "stremio-doctor (+https://github.com/dan-yates1/stremio-doctor)"

// Error kinds.
const (
	ErrTimeout = "timeout"
	ErrDNS     = "dns"
	ErrConnect = "connect"
	ErrTLS     = "tls"
	ErrNetwork = "network"
	ErrHTTP    = "http"
)

// Timing is the phase breakdown of one request.
type Timing struct {
	DNS     time.Duration `json:"dns"`
	Connect time.Duration `json:"connect"`
	TLS     time.Duration `json:"tls"`
	TTFB    time.Duration `json:"ttfb"` // request start → first response byte
	Total   time.Duration `json:"total"`
}

// Server estimates time the server spent thinking: TTFB minus connection setup.
func (t Timing) Server() time.Duration {
	s := t.TTFB - t.DNS - t.Connect - t.TLS
	if s < 0 {
		return 0
	}
	return s
}

// Network is the connection-setup portion (DNS + TCP + TLS).
func (t Timing) Network() time.Duration { return t.DNS + t.Connect + t.TLS }

// Download is the time spent receiving the body after the first byte.
func (t Timing) Download() time.Duration {
	if t.TTFB == 0 || t.Total < t.TTFB {
		return 0
	}
	return t.Total - t.TTFB
}

// Result is the outcome of one request.
type Result struct {
	Status     int    `json:"status,omitempty"`
	Err        string `json:"error,omitempty"`
	ErrKind    string `json:"errorKind,omitempty"`
	Timing     Timing `json:"timing"`
	Bytes      int    `json:"bytes"`
	Cloudflare bool   `json:"cloudflare,omitempty"`
	RemoteIP   string `json:"remoteIp,omitempty"`
}

// OK reports whether the request returned a 2xx response.
func (r Result) OK() bool { return r.Err == "" && r.Status >= 200 && r.Status < 300 }

// NewClient returns a client that opens a fresh connection per request so
// every result has a full DNS/connect/TLS breakdown.
func NewClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableKeepAlives = true
	return &http.Client{Transport: tr, Timeout: timeout}
}

// Get performs a timed GET and returns the result and (up to MaxBody of) the body.
func Get(ctx context.Context, client *http.Client, rawURL string) (Result, []byte) {
	var (
		res                        Result
		start                      = time.Now()
		dnsStart, connStart, tlsSt time.Time
	)
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:           func(httptrace.DNSDoneInfo) { res.Timing.DNS = since(dnsStart) },
		ConnectStart:      func(_, _ string) { connStart = time.Now() },
		ConnectDone:       func(_, _ string, _ error) { res.Timing.Connect = since(connStart) },
		TLSHandshakeStart: func() { tlsSt = time.Now() },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { res.Timing.TLS = since(tlsSt) },
		GotConn: func(info httptrace.GotConnInfo) {
			if addr, ok := info.Conn.RemoteAddr().(*net.TCPAddr); ok {
				res.RemoteIP = addr.IP.String()
			}
		},
		GotFirstResponseByte: func() { res.Timing.TTFB = time.Since(start) },
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, rawURL, nil)
	if err != nil {
		res.Err, res.ErrKind = err.Error(), ErrNetwork
		return res, nil
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		res.Timing.Total = time.Since(start)
		res.ErrKind, res.Err = Classify(err)
		return res, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	res.Timing.Total = time.Since(start)
	res.Status = resp.StatusCode
	res.Bytes = len(body)
	res.Cloudflare = isCloudflare(resp.Header)
	if err != nil {
		res.ErrKind, res.Err = Classify(err)
		return res, nil
	}
	if !res.OK() {
		res.ErrKind, res.Err = ErrHTTP, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return res, body
}

// Classify maps a transport error to a kind and a message. The message never
// contains the request URL (configured addon URLs embed secrets).
func Classify(err error) (kind, msg string) {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		if uerr.Timeout() {
			return ErrTimeout, "timed out"
		}
		err = uerr.Err
	}
	var (
		dnsErr  *net.DNSError
		certErr *tls.CertificateVerificationError
		unkAuth x509.UnknownAuthorityError
		hostErr x509.HostnameError
		invErr  x509.CertificateInvalidError
		recErr  tls.RecordHeaderError
		opErr   *net.OpError
	)
	switch {
	case errors.Is(err, context.DeadlineExceeded), os.IsTimeout(err):
		return ErrTimeout, "timed out"
	case errors.As(err, &dnsErr):
		return ErrDNS, "DNS lookup failed: " + dnsErr.Err
	case errors.As(err, &certErr), errors.As(err, &unkAuth), errors.As(err, &hostErr),
		errors.As(err, &invErr), errors.As(err, &recErr), strings.Contains(err.Error(), "tls:"):
		return ErrTLS, "TLS error: " + err.Error()
	case errors.As(err, &opErr) && opErr.Op == "dial":
		return ErrConnect, "connection failed: " + opErr.Err.Error()
	default:
		return ErrNetwork, err.Error()
	}
}

func isCloudflare(h http.Header) bool {
	return h.Get("Cf-Ray") != "" || strings.EqualFold(h.Get("Server"), "cloudflare")
}

func since(t time.Time) time.Duration {
	if t.IsZero() {
		return 0
	}
	return time.Since(t)
}
