package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/share"
)

// defaultShareEndpoint is empty until the community server exists.
const defaultShareEndpoint = ""

// shareEvery limits how often watch mode shares.
const shareEvery = time.Hour

// sharer sends the opt-in community report, at most once per shareEvery.
// It is used from one goroutine at a time.
type sharer struct {
	enabled, dryRun bool
	endpoint        string
	last            time.Time
}

func newSharer(cfg config) *sharer {
	return &sharer{enabled: cfg.share, dryRun: cfg.shareDryRun, endpoint: cfg.shareEndpoint}
}

// maybeShare shares rep if sharing is on and due. Failures are printed,
// never fatal.
func (s *sharer) maybeShare(ctx context.Context, out io.Writer, rep report.Report) {
	if !s.enabled && !s.dryRun || (!s.last.IsZero() && time.Since(s.last) < shareEvery) {
		return
	}
	s.last = time.Now()
	p, ok := share.Build(rep, version)
	if !ok {
		fmt.Fprintln(out, "Sharing: none of your addons are well-known public instances, so there's nothing to share.")
		return
	}
	if s.dryRun {
		fmt.Fprintln(out, "\nThis is exactly what --share would send (nothing was sent):")
		b, _ := json.MarshalIndent(p, "", "  ")
		fmt.Fprintln(out, string(b))
		return
	}
	if s.endpoint == "" {
		fmt.Fprintln(out, "Sharing: there's no community server yet, so nothing was sent. Use --share-dry-run to see what would be sent.")
		return
	}
	if err := share.Send(ctx, s.endpoint, p); err != nil {
		fmt.Fprintln(out, "Sharing failed:", err)
		return
	}
	fmt.Fprintf(out, "Shared anonymous results for %d public addon(s).\n", len(p.Addons))
}
