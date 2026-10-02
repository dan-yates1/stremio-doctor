package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/report"
	"github.com/dan-yates1/stremio-doctor/internal/scan"
)

func publicReport() report.Report {
	return report.Build(report.Input{Results: []scan.AddonResult{
		{Name: "Cinemeta", ID: "com.linvo.cinemeta", Host: "v3-cinemeta.strem.io"},
	}})
}

func TestSharerOffByDefault(t *testing.T) {
	var out bytes.Buffer
	newSharer(config{}).maybeShare(context.Background(), &out, publicReport())
	if out.Len() != 0 {
		t.Errorf("sharing ran without opt-in: %q", out.String())
	}
}

func TestSharerDryRunOncePerHour(t *testing.T) {
	var out bytes.Buffer
	s := newSharer(config{shareDryRun: true})
	s.maybeShare(context.Background(), &out, publicReport())
	if !strings.Contains(out.String(), `"com.linvo.cinemeta"`) {
		t.Fatalf("dry run didn't print the payload: %q", out.String())
	}
	out.Reset()
	s.maybeShare(context.Background(), &out, publicReport())
	if out.Len() != 0 {
		t.Error("shared twice within an hour")
	}
	s.last = time.Now().Add(-shareEvery - time.Minute)
	s.maybeShare(context.Background(), &out, publicReport())
	if out.Len() == 0 {
		t.Error("didn't share again after an hour")
	}
}

func TestSharerWithoutEndpointSendsNothing(t *testing.T) {
	var out bytes.Buffer
	newSharer(config{share: true}).maybeShare(context.Background(), &out, publicReport())
	if !strings.Contains(out.String(), "no community server yet") {
		t.Errorf("got %q", out.String())
	}
}
