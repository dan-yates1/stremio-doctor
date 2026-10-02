// Package share builds and sends the opt-in, anonymised community report:
// how well-known public addons performed from this user's network. It never
// includes addon URLs, names of private addons, or anything identifying.
package share

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/history"
	"github.com/dan-yates1/stremio-doctor/internal/probe"
	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

// SchemaVersion changes whenever Payload's meaning changes.
const SchemaVersion = 1

// sendTimeout bounds the upload; sharing must never hold up a scan.
const sendTimeout = 10 * time.Second

// Payload is everything that is sent.
type Payload struct {
	Schema   int       `json:"schema"`
	Tool     string    `json:"tool"`
	Hour     time.Time `json:"hour"` // scan time rounded down to the hour, UTC
	Baseline Baseline  `json:"baseline"`
	Addons   []Addon   `json:"addons"`
}

// Baseline separates "this addon is slow" from "this user's internet is
// slow". -1 means unreachable.
type Baseline struct {
	InternetMs   int64 `json:"internetMs"`
	StremioAPIMs int64 `json:"stremioApiMs"`
}

// Addon is one public addon's result.
type Addon struct {
	ID      string                 `json:"id"`
	Version string                 `json:"version"`
	Host    string                 `json:"host"`
	Checks  []history.CheckSummary `json:"checks"`
}

// Build selects the shareable part of a report. ok is false when there is
// nothing worth sending (no public addons).
func Build(rep report.Report, toolVersion string) (Payload, bool) {
	p := Payload{Schema: SchemaVersion, Tool: toolVersion, Hour: rep.Generated.UTC().Truncate(time.Hour),
		Baseline: Baseline{InternetMs: baselineMs(rep, 0), StremioAPIMs: baselineMs(rep, 1)}}
	for _, a := range rep.Addons {
		if a.ID == "" || !PublicHosts[a.Host] {
			continue
		}
		p.Addons = append(p.Addons, Addon{ID: a.ID, Version: a.Version, Host: a.Host, Checks: history.SummarizeChecks(a.Checks)})
	}
	return p, len(p.Addons) > 0
}

// baselineMs finds scan.BaselineTargets[i] in the report.
func baselineMs(rep report.Report, i int) int64 {
	for _, b := range rep.Baseline {
		if b.Name == scan.BaselineTargets[i].Name && b.Reachable() {
			return b.Result.Timing.Total.Milliseconds()
		}
	}
	return -1
}

// Send posts the payload as JSON.
func Send(ctx context.Context, endpoint string, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", probe.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("server answered HTTP %d", resp.StatusCode)
	}
	return nil
}
