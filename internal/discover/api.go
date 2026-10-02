package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dan-yates1/stremio-doctor/internal/addon"
	"github.com/dan-yates1/stremio-doctor/internal/probe"
)

// APIBase is the official Stremio API. Variable so tests can point it at a
// fake server.
var APIBase = "https://api.strem.io"

// FetchCollection reads the user's addon collection from the Stremio API.
// Read-only: this tool never calls addonCollectionSet.
func FetchCollection(ctx context.Context, authKey string) ([]addon.Installed, error) {
	body, _ := json.Marshal(map[string]any{
		"type": "AddonCollectionGet", "authKey": authKey, "update": true,
	})
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBase+"/api/addonCollectionGet", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", probe.UserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_, msg := probe.Classify(err)
		return nil, errors.New("Stremio API: " + msg)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, probe.MaxBody))
	if err != nil {
		return nil, err
	}

	var out struct {
		Result *struct {
			Addons []addon.Installed `json:"addons"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("Stremio API: unexpected response (HTTP %d)", resp.StatusCode)
	}
	if out.Error != nil {
		return nil, errors.New("Stremio API: " + out.Error.Message)
	}
	if out.Result == nil {
		return nil, fmt.Errorf("Stremio API: empty response (HTTP %d)", resp.StatusCode)
	}
	return out.Result.Addons, nil
}
